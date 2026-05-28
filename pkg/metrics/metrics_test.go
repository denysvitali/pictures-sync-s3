package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func newReg() *Registry { return NewRegistry() }

func TestIncIncrementsCounter(t *testing.T) {
	r := newReg()
	r.Inc("hits_total", nil)
	r.Inc("hits_total", nil)
	r.Inc("hits_total", nil)
	m := r.metrics["hits_total"]
	if got := m.series[""]; got != 3 {
		t.Fatalf("want 3, got %g", got)
	}
}

func TestSetUpdatesGauge(t *testing.T) {
	r := newReg()
	r.Set("temp", 42.5, nil)
	r.Set("temp", 99.1, nil)
	if got := r.metrics["temp"].series[""]; got != 99.1 {
		t.Fatalf("want 99.1, got %g", got)
	}
}

func TestLabelsCreateDistinctSeries(t *testing.T) {
	r := newReg()
	r.Inc("foo_total", map[string]string{"a": "1"})
	r.Inc("foo_total", map[string]string{"a": "1"})
	r.Inc("foo_total", map[string]string{"a": "2"})
	m := r.metrics["foo_total"]
	if got := m.series["a=1"]; got != 2 {
		t.Fatalf("want series a=1 == 2, got %g", got)
	}
	if got := m.series["a=2"]; got != 1 {
		t.Fatalf("want series a=2 == 1, got %g", got)
	}
}

func TestHandlerContentTypeAndBody(t *testing.T) {
	r := newReg()
	r.Inc("reqs_total", map[string]string{"method": "GET"})

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("unexpected Content-Type: %s", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# TYPE reqs_total counter") {
		t.Fatalf("missing TYPE line in body:\n%s", body)
	}
	if !strings.Contains(body, `reqs_total{method="GET"} 1`) {
		t.Fatalf("missing metric line in body:\n%s", body)
	}
}

func TestConcurrentIncCorrectTotal(t *testing.T) {
	r := newReg()
	const goroutines, each = 100, 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				r.Inc("concurrent_total", nil)
			}
		}()
	}
	wg.Wait()
	if got := r.metrics["concurrent_total"].series[""]; got != goroutines*each {
		t.Fatalf("want %d, got %g", goroutines*each, got)
	}
}

func TestLabelSpecialCharEscaping(t *testing.T) {
	cases := []struct {
		val  string
		want string
	}{
		{`foo"bar`, `foo\"bar`},
		{"foo\nbar", `foo\nbar`},
		{`foo\bar`, `foo\\bar`},
	}
	for _, tc := range cases {
		r := newReg()
		r.Inc("esc_total", map[string]string{"k": tc.val})

		req := httptest.NewRequest("GET", "/metrics", nil)
		rec := httptest.NewRecorder()
		r.Handler().ServeHTTP(rec, req)

		body := rec.Body.String()
		want := `k="` + tc.want + `"`
		if !strings.Contains(body, want) {
			t.Errorf("input %q: want %q in body, got:\n%s", tc.val, want, body)
		}
	}
}
