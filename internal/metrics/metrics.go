// Package metrics registers Dispatch's operator metrics with the
// controller-runtime registry (design §8).
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// SessionsTotal counts sessions reaching a terminal phase, by agent and
// outcome.
var SessionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "dispatch_sessions_total",
		Help: "Sessions that reached a terminal phase, by agent and outcome.",
	},
	[]string{"agent", "outcome"},
)

// EventsTotal counts gateway event dispositions, by source and outcome of
// the deterministic pipeline (design §5).
var EventsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "dispatch_events_total",
		Help: "Gateway events by source and disposition (filtered, deduped, suppressed, dispatched, error).",
	},
	[]string{"source", "disposition"},
)

// TokensTotal counts tokens a session reported, by agent, model, and
// direction. Sourced from the runner's harvested result rather than provider
// telemetry, so it works on subscription billing too (ADR-0004).
var TokensTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "dispatch_tokens_total",
		Help: "Tokens consumed by completed sessions, by agent, model, and direction.",
	},
	[]string{"agent", "model", "direction"},
)

// CostUSDTotal accumulates the API-equivalent cost of sessions, so a
// subscription-billed fleet can still answer what it would have cost.
var CostUSDTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "dispatch_cost_usd_total",
		Help: "API-equivalent cost of completed sessions in USD, by agent, model, and billing mode.",
	},
	[]string{"agent", "model", "billing"},
)

// SessionSeconds observes how long sessions ran, by agent and outcome.
var SessionSeconds = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "dispatch_session_seconds",
		Help:    "Wall-clock duration of sessions that reached a terminal phase.",
		Buckets: []float64{10, 30, 60, 300, 900, 1800, 3600},
	},
	[]string{"agent", "outcome"},
)

func init() {
	ctrlmetrics.Registry.MustRegister(SessionsTotal, EventsTotal, TokensTotal, CostUSDTotal, SessionSeconds)
}
