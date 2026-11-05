package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"rate-limiter/internal/limiter"
	"rate-limiter/internal/metrics"
	"rate-limiter/internal/storage"
)

func TestNewPolicyManagerSelectsAlgorithmsAndConfigurations(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer redisClient.Close()

	manager := newPolicyManager(redisClient)
	tests := []struct {
		name          string
		apiKey        string
		wantLimiter   any
		wantConfig    limiter.Config
		wantAlgorithm string
		wantTier      string
	}{
		{
			name:          "free",
			apiKey:        "nandini-free",
			wantLimiter:   (*storage.FixedWindow)(nil),
			wantConfig:    limiter.Config{Limit: 30, Window: time.Minute},
			wantAlgorithm: "fixed_window",
			wantTier:      "free",
		},
		{
			name:          "pro",
			apiKey:        "nandini-pro",
			wantLimiter:   (*storage.TokenBucket)(nil),
			wantConfig:    limiter.Config{Limit: 30, Window: time.Minute, BurstCapacity: 30},
			wantAlgorithm: "token_bucket",
			wantTier:      "pro",
		},
		{
			name:          "enterprise",
			apiKey:        "nandini-enterprise",
			wantLimiter:   (*storage.SlidingWindowCounter)(nil),
			wantConfig:    limiter.Config{Limit: 30, Window: time.Minute},
			wantAlgorithm: "sliding_window_counter",
			wantTier:      "enterprise",
		},
		{
			name:          "custom log",
			apiKey:        "nandini-log",
			wantLimiter:   (*storage.SlidingWindowLog)(nil),
			wantConfig:    limiter.Config{Limit: 30, Window: time.Minute},
			wantAlgorithm: "sliding_window_log",
			wantTier:      "custom",
		},
		{
			name:          "custom leaky",
			apiKey:        "nandini-leaky",
			wantLimiter:   (*storage.LeakyBucket)(nil),
			wantConfig:    limiter.Config{Limit: 30, Window: time.Minute, BurstCapacity: 30},
			wantAlgorithm: "leaky_bucket",
			wantTier:      "custom",
		},
		{
			name:          "unknown defaults to free",
			apiKey:        "unknown-key",
			wantLimiter:   (*storage.FixedWindow)(nil),
			wantConfig:    limiter.Config{Limit: 30, Window: time.Minute},
			wantAlgorithm: "fixed_window",
			wantTier:      "free",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := manager.Get(test.apiKey)
			if gotType := reflect.TypeOf(got.Limiter); gotType != reflect.TypeOf(test.wantLimiter) {
				t.Errorf("limiter type = %v, want %v", gotType, reflect.TypeOf(test.wantLimiter))
			}
			if got.Config != test.wantConfig {
				t.Errorf("config = %+v, want %+v", got.Config, test.wantConfig)
			}
			if got.Algorithm != test.wantAlgorithm {
				t.Errorf("algorithm = %q, want %q", got.Algorithm, test.wantAlgorithm)
			}
			if got.Tier != test.wantTier {
				t.Errorf("tier = %q, want %q", got.Tier, test.wantTier)
			}
		})
	}
}

func TestRedisAddressConfiguration(t *testing.T) {
	t.Run("uses configured address", func(t *testing.T) {
		t.Setenv("REDIS_ADDR", "redis:6379")
		if got := redisAddress(); got != "redis:6379" {
			t.Errorf("redisAddress() = %q, want %q", got, "redis:6379")
		}
	})

	t.Run("defaults to local Redis", func(t *testing.T) {
		t.Setenv("REDIS_ADDR", "")
		if got := redisAddress(); got != "localhost:6379" {
			t.Errorf("redisAddress() = %q, want %q", got, "localhost:6379")
		}
	})
}

func TestMetricsEndpoint(t *testing.T) {
	metrics.Init()
	metrics.RequestsTotal.WithLabelValues("allowed", "endpoint_test", "test").Inc()

	handler := newMux(nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want Prometheus text format", contentType)
	}
	if !strings.Contains(response.Body.String(), "ratelimitx_requests_total{") {
		t.Errorf("metrics output does not contain the request counter: %s", response.Body.String())
	}
	if _, err := prometheus.DefaultGatherer.Gather(); err != nil {
		t.Errorf("gather registered metrics: %v", err)
	}
}

func TestIndexPage(t *testing.T) {
	handler := newMux(nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/html; charset=utf-8")
	}
	if !strings.Contains(response.Body.String(), "<title>RateLimitX") {
		t.Error("response does not contain the RateLimitX page")
	}
}
