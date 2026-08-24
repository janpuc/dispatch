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

func init() {
	ctrlmetrics.Registry.MustRegister(SessionsTotal)
}
