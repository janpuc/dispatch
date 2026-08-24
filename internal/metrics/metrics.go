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

func init() {
	ctrlmetrics.Registry.MustRegister(SessionsTotal, EventsTotal)
}
