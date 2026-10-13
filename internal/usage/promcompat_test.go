//go:build promcompat

/*
Copyright 2026 Machani Vidyasagar.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package usage

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestRealPrometheus runs the provider against a real Prometheus (F-0306: Prometheus LTS and latest in
// CI). It is excluded from `make test`; the "Prometheus compatibility" workflow starts Prometheus and runs:
//
//	PROMETHEUS_URL=http://localhost:9090 go test -tags=promcompat ./internal/usage/ -run TestRealPrometheus -v
//
// Prometheus scrapes itself, so the series with job="prometheus" stand in for a namespace: the namespace
// label is set to "job" for this test.
func TestRealPrometheus(t *testing.T) {
	url := os.Getenv("PROMETHEUS_URL")
	if url == "" {
		t.Skip("PROMETHEUS_URL is not set")
	}
	p, err := NewPrometheusProvider(Settings{URL: url, NamespaceLabel: "job", QueryTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// The first scrape takes a few seconds after start.
	var m Measurement
	deadline := time.Now().Add(90 * time.Second)
	for {
		m, err = p.Measure(ctx, "prometheus")
		if err == nil && m.ActiveSeries > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no valid measurement from %s: %v (series %d)", url, err, m.ActiveSeries)
		}
		time.Sleep(3 * time.Second)
	}
	if len(m.TopMetrics) == 0 || m.TopMetrics[0].Series < m.TopMetrics[len(m.TopMetrics)-1].Series {
		t.Errorf("top metrics missing or not sorted: %+v", m.TopMetrics)
	}

	up, err := p.MetricSeries(ctx, "prometheus", "up")
	if err != nil || up < 1 {
		t.Errorf("MetricSeries(up) = %d, %v; want at least 1", up, err)
	}

	reloaded, err := p.ConfigReloadedSince(ctx, time.Now().Add(-24*time.Hour))
	if err != nil || !reloaded {
		t.Errorf("ConfigReloadedSince = %v, %v; want true (runtimeinfo fields startTime, reloadConfigSuccess, lastConfigTime)", reloaded, err)
	}
	t.Logf("%s: %d series, top metric %s (%d)", url, m.ActiveSeries, m.TopMetrics[0].Name, m.TopMetrics[0].Series)
}
