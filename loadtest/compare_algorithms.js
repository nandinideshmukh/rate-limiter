import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const BASE_URL = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/+$/, '');
const ENDPOINT = `${BASE_URL}/api/loadtest`;
const DURATION = '60s';
const VUS = 10;

const policies = {
  fixed_window: {
    apiKey: __ENV.FIXED_WINDOW_API_KEY || 'nandini-free',
    tier: 'free',
  },
  sliding_window_log: {
    apiKey: __ENV.SLIDING_WINDOW_LOG_API_KEY || 'nandini-log',
    tier: 'custom',
  },
  sliding_window_counter: {
    apiKey: __ENV.SLIDING_WINDOW_COUNTER_API_KEY || 'nandini-enterprise',
    tier: 'enterprise',
  },
  token_bucket: {
    apiKey: __ENV.TOKEN_BUCKET_API_KEY || 'nandini-pro',
    tier: 'pro',
  },
  leaky_bucket: {
    apiKey: __ENV.LEAKY_BUCKET_API_KEY || 'nandini-leaky',
    tier: 'custom',
  },
};

http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 429));

const unexpectedHttpErrors = new Counter('comparison_unexpected_http_errors');
const successfulRequests = new Counter('comparison_successful_requests');
const http429Responses = new Counter('comparison_http_429_responses');
const http5xxResponses = new Counter('comparison_http_5xx_responses');

const offsets = ['0s', '65s', '130s', '195s', '260s'];
const algorithms = Object.keys(policies);
const scenarios = {};

function getHeader(response, name) {
  const wanted = name.toLowerCase();
  const match = Object.entries(response.headers).find(
    ([headerName]) => headerName.toLowerCase() === wanted,
  );
  return match ? match[1] : undefined;
}

algorithms.forEach((algorithm, index) => {
  scenarios[algorithm] = {
    executor: 'constant-vus',
    exec: `run_${algorithm}`,
    vus: VUS,
    duration: DURATION,
    startTime: offsets[index],
    tags: { algorithm, tier: policies[algorithm].tier },
  };
});

export const options = {
  scenarios,
  thresholds: {
    comparison_unexpected_http_errors: ['count==0'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  discardResponseBodies: true,
};

export function setup() {
  const observedLimits = {};

  algorithms.forEach((algorithm) => {
    const policy = policies[algorithm];
    const response = http.get(ENDPOINT, {
      headers: { 'X-API-Key': policy.apiKey },
    });
    const rawLimit = getHeader(response, 'X-RateLimit-Limit');
    const limit = Number(rawLimit);

    if (!((response.status >= 200 && response.status < 300) || response.status === 429)) {
      throw new Error(
        `Policy probe for ${algorithm} returned HTTP ${response.status}; check BASE_URL and API key.`,
      );
    }

    if (!Number.isFinite(limit) || limit <= 0) {
      throw new Error(
        `Policy probe for ${algorithm} did not return X-RateLimit-Limit ` +
          `(HTTP ${response.status}; headers: ${JSON.stringify(response.headers)}); ` +
          'comparison cannot verify policy limits.',
      );
    }

    observedLimits[algorithm] = limit;
  });

  const distinctLimits = [...new Set(Object.values(observedLimits))];
  if (distinctLimits.length !== 1) {
    throw new Error(
      `Unfair algorithm comparison: observed limits differ (${JSON.stringify(observedLimits)}). ` +
        'Set identical Limit and Window values for all five policies in cmd/server/main.go; ' +
        'also align BurstCapacity for token_bucket and leaky_bucket, then restart RateLimitX.',
    );
  }
}

function runAlgorithm(algorithm) {
  const policy = policies[algorithm];
  const tags = { algorithm, tier: policy.tier };
  const response = http.get(ENDPOINT, {
    headers: { 'X-API-Key': policy.apiKey },
    tags,
  });
  const status = response.status;
  const isSuccessful = status >= 200 && status < 300;
  const expected = isSuccessful || status === 429;

  check(response, {
    'response is 2xx or 429': () => expected,
  });

  if (isSuccessful) {
    successfulRequests.add(1, tags);
  } else if (status === 429) {
    http429Responses.add(1, tags);
  } else if (status >= 500 && status < 600) {
    http5xxResponses.add(1, tags);
  }

  if (!expected) {
    unexpectedHttpErrors.add(1, tags);
  }
}

export function run_fixed_window() {
  runAlgorithm('fixed_window');
}

export function run_sliding_window_log() {
  runAlgorithm('sliding_window_log');
}

export function run_sliding_window_counter() {
  runAlgorithm('sliding_window_counter');
}

export function run_token_bucket() {
  runAlgorithm('token_bucket');
}

export function run_leaky_bucket() {
  runAlgorithm('leaky_bucket');
}
