import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const BASE_URL = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/+$/, '');
const API_KEY = __ENV.API_KEY || 'nandini-enterprise';
const ENDPOINT = `${BASE_URL}/api/loadtest`;

http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 429));

const unexpectedHttpErrors = new Counter('unexpected_http_errors');
const http429Responses = new Counter('http_429_responses');
const http5xxResponses = new Counter('http_5xx_responses');

export const options = {
  scenarios: {
    basic: {
      executor: 'constant-vus',
      vus: 10,
      duration: '30s',
    },
  },
  thresholds: {
    unexpected_http_errors: ['count==0'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  discardResponseBodies: true,
};

export default function () {
  const response = http.get(ENDPOINT, {
    headers: { 'X-API-Key': API_KEY },
  });
  const status = response.status;
  const expected = (status >= 200 && status < 300) || status === 429;

  check(response, {
    'response is 2xx or 429': () => expected,
  });

  if (status === 429) {
    http429Responses.add(1);
  } else if (status >= 500 && status < 600) {
    http5xxResponses.add(1);
  }

  if (!expected) {
    unexpectedHttpErrors.add(1);
  }
}
