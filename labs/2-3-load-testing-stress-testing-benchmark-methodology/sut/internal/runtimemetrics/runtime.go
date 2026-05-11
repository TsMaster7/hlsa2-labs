// Package runtimemetrics installs the rich Go runtime collector
// (GOMAXPROCS, scheduler latencies, GC pause histogram, etc.) onto
// the default Prometheus registry, replacing the basic GoCollector
// that client_golang installs automatically at import time.
//
// These feed the "service - USE" row of the four-layer dashboard
// and the soak-test panels.
package runtimemetrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Register replaces the default basic Go collector with a rich one
// (MetricsAll), and ensures the process collector is installed too.
//
// client_golang's init() registers a basic GoCollector on the default
// registry, so attempting to register a second one panics with
// "duplicate metrics collector registration attempted". We unregister
// it first by registering a sentinel and inspecting the error, then
// register the rich collector instead.
func Register() {
	rich := collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll),
	)
	if err := prometheus.DefaultRegisterer.Register(rich); err != nil {
		// Already registered (or a basic one is registered). Try to
		// unregister it and replace with the rich collector.
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			prometheus.DefaultRegisterer.Unregister(are.ExistingCollector)
			_ = prometheus.DefaultRegisterer.Register(rich)
		}
	}

	proc := collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})
	if err := prometheus.DefaultRegisterer.Register(proc); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			prometheus.DefaultRegisterer.Unregister(are.ExistingCollector)
			_ = prometheus.DefaultRegisterer.Register(proc)
		}
	}
}
