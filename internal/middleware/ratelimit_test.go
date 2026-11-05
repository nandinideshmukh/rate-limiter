package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"rate-limiter/internal/limiter"
	"rate-limiter/internal/metrics"
	"rate-limiter/internal/policy"
)

type stubLimiter struct {
	decision limiter.Decision
	err      error
}

func (s stubLimiter) Allow(context.Context, string, limiter.Config) (limiter.Decision, error) {
	return s.decision, s.err
}

func TestRateLimitHTTPResponses(t *testing.T) {
	config := limiter.Config{Limit: 5, Window: time.Minute}
	tests := []struct {
		name            string
		decision        limiter.Decision
		err             error
		wantStatus      int
		wantLimit       string
		wantRemaining   string
		wantReset       string
		wantRetryAfter  string
		wantBody        string
		wantHandlerCall bool
	}{
		{
			name:            "allowed",
			decision:        limiter.Decision{Allowed: true, Remaining: 4, ResetAt: time.Unix(120, 0)},
			wantStatus:      http.StatusOK,
			wantLimit:       "5",
			wantRemaining:   "4",
			wantReset:       "120",
			wantHandlerCall: true,
		},
		{
			name:           "rejected",
			decision:       limiter.Decision{Allowed: false, Remaining: 0, RetryAfter: 1500 * time.Millisecond},
			wantStatus:     http.StatusTooManyRequests,
			wantLimit:      "5",
			wantRemaining:  "0",
			wantRetryAfter: "2",
			wantBody:       "rate limit exceeded",
		},
		{
			name:       "limiter failure",
			err:        errors.New("Redis unavailable"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "Rate limiter temporarily unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := policy.NewManager(policy.Policy{
				Limiter: stubLimiter{decision: test.decision, err: test.err},
				Config:  config,
			}, nil)
			handlerCalled := false
			handler := RateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			}), manager)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if got := response.Header().Get("X-RateLimit-Limit"); got != test.wantLimit {
				t.Errorf("X-RateLimit-Limit = %q, want %q", got, test.wantLimit)
			}
			if got := response.Header().Get("X-RateLimit-Remaining"); got != test.wantRemaining {
				t.Errorf("X-RateLimit-Remaining = %q, want %q", got, test.wantRemaining)
			}
			if got := response.Header().Get("X-RateLimit-Reset"); got != test.wantReset {
				t.Errorf("X-RateLimit-Reset = %q, want %q", got, test.wantReset)
			}
			if got := response.Header().Get("Retry-After"); got != test.wantRetryAfter {
				t.Errorf("Retry-After = %q, want %q", got, test.wantRetryAfter)
			}
			if test.wantBody != "" && !strings.Contains(response.Body.String(), test.wantBody) {
				t.Errorf("body = %q, want it to contain %q", response.Body.String(), test.wantBody)
			}
			if handlerCalled != test.wantHandlerCall {
				t.Errorf("handler called = %t, want %t", handlerCalled, test.wantHandlerCall)
			}
		})
	}
}

func TestAPIKeysProduceIndependentStableClientKeys(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/resource", nil)

	first := getClientKey(request, "first-key")
	if got := getClientKey(request, "first-key"); got != first {
		t.Errorf("same API key produced different client keys: %q and %q", first, got)
	}
	if second := getClientKey(request, "second-key"); second == first {
		t.Error("different API keys produced the same client key")
	}
	if strings.Contains(first, "first-key") {
		t.Error("client key contains the raw API key")
	}
}

func TestRateLimitRecordsMetrics(t *testing.T) {
	metrics.Init()
	tests := []struct {
		name      string
		algorithm string
		tier      string
		decision  limiter.Decision
		err       error
		status    string
		rejected  bool
	}{
		{
			name:      "allowed",
			algorithm: "metrics_test_fixed_window",
			tier:      "metrics_test_free",
			decision:  limiter.Decision{Allowed: true},
			status:    "allowed",
		},
		{
			name:      "rejected",
			algorithm: "metrics_test_sliding_window_log",
			tier:      "metrics_test_custom",
			decision:  limiter.Decision{Allowed: false},
			status:    "rejected",
			rejected:  true,
		},
		{
			name:      "limiter error",
			algorithm: "metrics_test_token_bucket",
			tier:      "metrics_test_pro",
			err:       errors.New("Redis unavailable"),
			status:    "error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforeRequests := counterValue(t, metrics.RequestsTotal.WithLabelValues(test.status, test.algorithm, test.tier))
			beforeRejected := counterValue(t, metrics.RejectedRequests.WithLabelValues(test.algorithm, test.tier))
			beforeDuration := histogramSampleCount(t, test.algorithm)

			manager := policy.NewManager(policy.Policy{
				Limiter:   stubLimiter{decision: test.decision, err: test.err},
				Algorithm: test.algorithm,
				Tier:      test.tier,
			}, nil)
			handler := RateLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), manager)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resource", nil))

			if got := counterValue(t, metrics.RequestsTotal.WithLabelValues(test.status, test.algorithm, test.tier)); got != beforeRequests+1 {
				t.Errorf("request counter = %v, want %v", got, beforeRequests+1)
			}

			wantRejected := beforeRejected
			if test.rejected {
				wantRejected++
			}
			if got := counterValue(t, metrics.RejectedRequests.WithLabelValues(test.algorithm, test.tier)); got != wantRejected {
				t.Errorf("rejected counter = %v, want %v", got, wantRejected)
			}

			if got := histogramSampleCount(t, test.algorithm); got != beforeDuration+1 {
				t.Errorf("duration sample count = %d, want %d", got, beforeDuration+1)
			}
		})
	}
}

func counterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	metric := &dto.Metric{}
	if err := counter.Write(metric); err != nil {
		t.Fatalf("write counter metric: %v", err)
	}
	return metric.GetCounter().GetValue()
}

func histogramSampleCount(t *testing.T, algorithm string) uint64 {
	t.Helper()
	observer, err := metrics.RequestDuration.GetMetricWithLabelValues(algorithm)
	if err != nil {
		t.Fatalf("get duration metric: %v", err)
	}
	metric, ok := observer.(prometheus.Metric)
	if !ok {
		t.Fatalf("duration observer %T does not implement prometheus.Metric", observer)
	}
	dtoMetric := &dto.Metric{}
	if err := metric.Write(dtoMetric); err != nil {
		t.Fatalf("write duration metric: %v", err)
	}
	return dtoMetric.GetHistogram().GetSampleCount()
}
