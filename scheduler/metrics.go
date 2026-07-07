package scheduler

import "github.com/prometheus/client_golang/prometheus"

// Metrics are package-level singletons, registered exactly once regardless
// of how many Runners a process constructs — constructing a fresh
// CounterVec/HistogramVec per Runner (mirroring metrics.NewRequestMetrics)
// would panic on prometheus.MustRegister the second time a Runner is built
// in the same process (e.g. in tests, or a hypothetical multi-Runner
// service), so job identity is carried entirely by the "job" label instead.
var (
	runsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_runs_total",
		Help: "Total scheduler job runs, by job name.",
	}, []string{"job"})

	failuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_failures_total",
		Help: "Total scheduler job runs that returned an error (or panicked), by job name.",
	}, []string{"job"})

	durationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "scheduler_duration_seconds",
		Help:    "Scheduler job run duration in seconds, by job name.",
		Buckets: prometheus.DefBuckets,
	}, []string{"job"})

	skippedSingletonTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scheduler_skipped_singleton_total",
		Help: "Total scheduler job ticks skipped because another replica held the WithSingleton advisory lock, by job name.",
	}, []string{"job"})
)

func init() {
	prometheus.MustRegister(runsTotal, failuresTotal, durationSeconds, skippedSingletonTotal)
}
