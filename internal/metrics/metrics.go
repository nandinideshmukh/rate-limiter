package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ratelimitx_requests_total",
			Help: "Total number of requests processed",
		},
		[]string{"status", "algorithm", "tier"},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ratelimitx_request_duration_seconds",
			Help:    "Request processing duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"algorithm"},
	)

	RejectedRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ratelimitx_rejected_requests_total",
			Help: "Total number of rate-limited requests",
		},
		[]string{"algorithm", "tier"},
	)
)

var initOnce sync.Once

func Init() {
	initOnce.Do(func() {
		prometheus.MustRegister(
			RequestsTotal,
			RequestDuration,
			RejectedRequests,
		)
	})
}
