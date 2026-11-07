package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"rate-limiter/internal/limiter"
	"rate-limiter/internal/metrics"
	"rate-limiter/internal/middleware"
	"rate-limiter/internal/policy"
	"rate-limiter/internal/storage"
	"rate-limiter/web"
)

func main() {

	// Connect to Redis.
	redisClient, err := storage.NewRedisClient(redisAddress())
	if err != nil {
		log.Fatal(err)
	}
	defer redisClient.Close()

	manager := newPolicyManager(redisClient.Client)

	metrics.Init()
	mux := newMux(manager)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Println("RateLimitX running on port", port)

	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func newMux(manager *policy.Manager) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(web.IndexHTML))
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Server is healthy!")
	})

	mux.Handle("/health/prometheus", healthCheckHandler(
		"http://prometheus:9090/-/healthy",
		"Prometheus",
	))

	mux.Handle("/health/grafana", healthCheckHandler(
		"http://grafana:3000/api/health",
		"Grafana",
	))

	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instance := os.Getenv("INSTANCE_ID")
		fmt.Fprintf(w, "Request handled by %s\n", instance)
	})

	mux.Handle("/api/", middleware.RateLimit(apiHandler, manager))
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

func healthCheckHandler(targetURL, serviceName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			targetURL,
			nil,
		)
		if err != nil {
			http.Error(w, serviceName+" unavailable", http.StatusServiceUnavailable)
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, serviceName+" unavailable", http.StatusServiceUnavailable)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			http.Error(w, serviceName+" unhealthy", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, serviceName+" is healthy")
	})
}

func redisAddress() string {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		return "localhost:6379"
	}
	return addr
}

func newPolicyManager(redisClient *redis.Client) *policy.Manager {
	fixedWindow := storage.NewRedisFixedWindow(redisClient)
	slidingWindowLog := storage.NewRedisSlidingWindowLog(redisClient)
	slidingWindowCounter := storage.NewRedisSlidingWindowCounter(redisClient)
	tokenBucket := storage.NewRedisTokenBucket(redisClient)
	leakyBucket := storage.NewRedisLeakyBucket(redisClient)

	freePolicy := policy.Policy{
		Limiter:   fixedWindow,
		Config:    limiter.Config{Limit: 30, Window: time.Minute},
		Algorithm: "fixed_window",
		Tier:      "free",
	}
	proPolicy := policy.Policy{
		Limiter:   tokenBucket,
		Config:    limiter.Config{Limit: 30, Window: time.Minute, BurstCapacity: 30},
		Algorithm: "token_bucket",
		Tier:      "pro",
	}
	enterprisePolicy := policy.Policy{
		Limiter:   slidingWindowCounter,
		Config:    limiter.Config{Limit: 30, Window: time.Minute},
		Algorithm: "sliding_window_counter",
		Tier:      "enterprise",
	}
	logPolicy := policy.Policy{
		Limiter:   slidingWindowLog,
		Config:    limiter.Config{Limit: 30, Window: time.Minute},
		Algorithm: "sliding_window_log",
		Tier:      "custom",
	}
	leakyPolicy := policy.Policy{
		Limiter:   leakyBucket,
		Config:    limiter.Config{Limit: 30, Window: time.Minute, BurstCapacity: 30},
		Algorithm: "leaky_bucket",
		Tier:      "custom",
	}

	return policy.NewManager(freePolicy, map[string]policy.Policy{
		"nandini-free":       freePolicy,
		"nandini-pro":        proPolicy,
		"nandini-enterprise": enterprisePolicy,
		"nandini-log":        logPolicy,
		"nandini-leaky":      leakyPolicy,
	})
}
