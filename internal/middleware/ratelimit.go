package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rate-limiter/internal/metrics"
	"rate-limiter/internal/policy"
)

func RateLimit(
	next http.Handler,
	manager *policy.Manager,
) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))

		// Identify the caller.
		key := getClientKey(r, apiKey)

		// Get the policy assigned to this API key.
		selectedPolicy := manager.Get(apiKey)
		duration := metrics.RequestDuration.WithLabelValues(selectedPolicy.Algorithm)
		defer func() {
			duration.Observe(time.Since(start).Seconds())
		}()

		// Execute the selected limiter.
		decision, err := selectedPolicy.Limiter.Allow(
			r.Context(),
			key,
			selectedPolicy.Config,
		)

		if err != nil {
			metrics.RequestsTotal.WithLabelValues(
				"error",
				selectedPolicy.Algorithm,
				selectedPolicy.Tier,
			).Inc()
			http.Error(
				w,
				"Rate limiter temporarily unavailable",
				http.StatusServiceUnavailable,
			)
			return
		}

		config := selectedPolicy.Config

		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(config.Limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))

		if !decision.ResetAt.IsZero() {
			w.Header().Set(
				"X-RateLimit-Reset",
				strconv.FormatInt(decision.ResetAt.Unix(), 10),
			)
		}

		if !decision.Allowed {
			metrics.RequestsTotal.WithLabelValues(
				"rejected",
				selectedPolicy.Algorithm,
				selectedPolicy.Tier,
			).Inc()
			metrics.RejectedRequests.WithLabelValues(
				selectedPolicy.Algorithm,
				selectedPolicy.Tier,
			).Inc()

			retrySeconds := int(math.Ceil(decision.RetryAfter.Seconds()))

			if retrySeconds < 1 {
				retrySeconds = 1
			}

			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			w.Header().Set("Content-Type", "application/json")

			w.WriteHeader(http.StatusTooManyRequests)

			json.NewEncoder(w).Encode(map[string]string{
				"error":       "rate limit exceeded",
				"retry_after": strconv.Itoa(retrySeconds) + " seconds",
			})

			return
		}

		metrics.RequestsTotal.WithLabelValues(
			"allowed",
			selectedPolicy.Algorithm,
			selectedPolicy.Tier,
		).Inc()
		next.ServeHTTP(w, r)
	})
}

func getClientKey(r *http.Request, apiKey string) string {

	if apiKey != "" {
		hash := sha256.Sum256([]byte(apiKey))
		return "apikey:" + hex.EncodeToString(hash[:])
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	return "ip:" + ip
}
