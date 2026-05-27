// Package metrics provides a minimal self-contained Prometheus-format exporter.
// It uses only the standard library — no prometheus/client_golang dependency.
// Callers use Inc and Set to update counters/gauges; Handler exposes them at
// /metrics in Prometheus text exposition format (version 0.0.4).
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Registry holds all metrics. Use the package-level DefaultRegistry for
// convenience, or create an isolated one for testing.
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]*metric
}

type metricKind int

const (
	kindCounter metricKind = iota
	kindGauge
)

type metric struct {
	kind   metricKind
	series map[string]float64 // label-set key → value
}

// DefaultRegistry is the process-wide default registry.
var DefaultRegistry = NewRegistry()

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		metrics: make(map[string]*metric),
	}
}

// labelKey builds a deterministic string key from a label map for use as a
// map key. Labels are sorted alphabetically so order doesn't matter.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

// Inc increments a counter metric by 1.
func (r *Registry) Inc(name string, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.getOrCreate(name, kindCounter)
	key := labelKey(labels)
	m.series[key]++
}

// Set assigns a gauge metric to the given value.
func (r *Registry) Set(name string, value float64, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.getOrCreate(name, kindGauge)
	key := labelKey(labels)
	m.series[key] = value
}

// getOrCreate returns the metric entry, creating it if absent.
// Caller must hold r.mu (write lock).
func (r *Registry) getOrCreate(name string, kind metricKind) *metric {
	if m, ok := r.metrics[name]; ok {
		return m
	}
	m := &metric{kind: kind, series: make(map[string]float64)}
	r.metrics[name] = m
	return m
}

// WriteMetrics writes all metrics to w in Prometheus text exposition format.
func (r *Registry) WriteMetrics(w io.Writer) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Collect and sort metric names for deterministic output.
	names := make([]string, 0, len(r.metrics))
	for name := range r.metrics {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		m := r.metrics[name]
		kindStr := "gauge"
		if m.kind == kindCounter {
			kindStr = "counter"
		}
		fmt.Fprintf(w, "# TYPE %s %s\n", name, kindStr)

		// Sort label keys for deterministic output.
		labelKeys := make([]string, 0, len(m.series))
		for lk := range m.series {
			labelKeys = append(labelKeys, lk)
		}
		sort.Strings(labelKeys)

		for _, lk := range labelKeys {
			val := m.series[lk]
			if lk == "" {
				fmt.Fprintf(w, "%s %g\n", name, val)
			} else {
				fmt.Fprintf(w, "%s{%s} %g\n", name, prometheusLabels(lk), val)
			}
		}
	}
}

// prometheusLabels converts the internal label key format (k=v,k=v) to
// Prometheus label syntax (k="v",k="v").
func prometheusLabels(lk string) string {
	var b strings.Builder
	pairs := strings.Split(lk, ",")
	for i, pair := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		idx := strings.IndexByte(pair, '=')
		if idx < 0 {
			b.WriteString(pair)
			continue
		}
		key := pair[:idx]
		val := pair[idx+1:]
		b.WriteString(key)
		b.WriteString(`="`)
		// Escape backslash, double-quote, and newline per the spec.
		for _, ch := range val {
			switch ch {
			case '\\':
				b.WriteString(`\\`)
			case '"':
				b.WriteString(`\"`)
			case '\n':
				b.WriteString(`\n`)
			default:
				b.WriteRune(ch)
			}
		}
		b.WriteByte('"')
	}
	return b.String()
}

// Handler returns an http.Handler that serves the registry in Prometheus format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.WriteMetrics(w)
	})
}

// --- Package-level helpers that delegate to DefaultRegistry ---

// Inc increments a counter in the default registry.
func Inc(name string, labels map[string]string) {
	DefaultRegistry.Inc(name, labels)
}

// Set assigns a gauge in the default registry.
func Set(name string, value float64, labels map[string]string) {
	DefaultRegistry.Set(name, value, labels)
}

// Handler returns the HTTP handler for the default registry.
func Handler() http.Handler {
	return DefaultRegistry.Handler()
}

// HTTPMiddleware wraps h and records pictures_webui_requests_total{method,status}
// and pictures_uptime_seconds in the default registry on every request.
func HTTPMiddleware(startTime time.Time, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &captureWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rw, r)

		// Update uptime gauge on each request (cheap, avoids a separate goroutine).
		Set("pictures_uptime_seconds", time.Since(startTime).Seconds(), nil)

		Inc("pictures_webui_requests_total", map[string]string{
			"method": r.Method,
			"status": fmt.Sprintf("%d", rw.code),
		})
	})
}

// captureWriter records the status code written by the wrapped handler.
type captureWriter struct {
	http.ResponseWriter
	code        int
	wroteHeader bool
}

func (cw *captureWriter) WriteHeader(code int) {
	if !cw.wroteHeader {
		cw.code = code
		cw.wroteHeader = true
	}
	cw.ResponseWriter.WriteHeader(code)
}

func (cw *captureWriter) Write(b []byte) (int, error) {
	if !cw.wroteHeader {
		cw.code = http.StatusOK
		cw.wroteHeader = true
	}
	return cw.ResponseWriter.Write(b)
}
