// Package scheduler standardizes in-process periodic work: a Job registered
// with a Runner gets panic recovery, Prometheus metrics, jittered first
// tick, and (opt-in via WithSingleton) a non-blocking Postgres advisory lock
// so only one replica actually runs a given tick.
//
// There is no cron-expression engine here by design: use the in-process
// Runner for sub-minute/stateful loops (outbox drain, cache refresh, token
// rotation); use a Kubernetes CronJob for infrequent wall-clock batch work
// (daily cleanup, reports) instead of adding a cron parser here.
package scheduler

import (
	"context"
	"time"
)

// Job is one unit of periodic work registered with a Runner.
type Job struct {
	// Name identifies the job in metrics/logs and, under WithSingleton, as
	// the advisory-lock key. Namespace it per service (e.g.
	// "acl-outbox-dispatch") — two unrelated jobs across services sharing
	// one Postgres instance would otherwise risk colliding on the same
	// derived lock key.
	Name string
	// Every is the steady-state interval between runs. Must be positive.
	Every time.Duration
	// Jitter randomizes the first tick's delay, in [0, Jitter) — staggers
	// many replicas that all start at once. Zero means no jitter.
	Jitter time.Duration
	// Timeout bounds each Run call via context. Zero means no timeout.
	Timeout time.Duration
	// Run performs one unit of work. A returned error is logged and counted
	// (failures_total) but never stops the Runner; Run should not panic —
	// the Runner recovers from one, but a panicking Run is still a bug.
	Run func(context.Context) error
}
