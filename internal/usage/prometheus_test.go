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
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/machani/cardinality-budget-operator/internal/testutil"
)

const (
	testNamespace = "payments"
	// defaultExclude is the ADR 0013 default (kept in sync with internal/config).
	defaultExclude = `job!~"apiserver|coredns|kube-state-metrics|kubelet|node-exporter",job!=""`
)

func excludeMatchers(t *testing.T, s string) []*labels.Matcher {
	t.Helper()
	m, err := parser.NewParser(parser.Options{}).ParseMetricSelector("{" + s + "}")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func newProvider(t *testing.T, fake *testutil.FakePrometheus, namespaceLabel string) *PrometheusProvider {
	t.Helper()
	p, err := NewPrometheusProvider(Settings{
		URL:             fake.URL(),
		NamespaceLabel:  namespaceLabel,
		ExcludeMatchers: excludeMatchers(t, defaultExclude),
		QueryTimeout:    5 * time.Second,
		BackendWarmup:   15 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func expectInvalid(t *testing.T, err error, want InvalidReason) {
	t.Helper()
	var invalid *InvalidError
	if !errors.As(err, &invalid) || invalid.Reason != want {
		t.Fatalf("error = %v, want InvalidError with reason %s", err, want)
	}
}

func TestQueriesUseNamespaceAndExclusions(t *testing.T) {
	fake := testutil.NewFakePrometheus()
	defer fake.Close()
	p := newProvider(t, fake, "namespace")

	want := `{namespace="payments",job!~"apiserver|coredns|kube-state-metrics|kubelet|node-exporter",job!=""}`
	if got := p.Selector(testNamespace); got != want {
		t.Errorf("selector = %s, want %s", got, want)
	}
	if got := p.CountQuery(testNamespace); got != "count("+want+")" {
		t.Errorf("count query = %s", got)
	}
	if got := p.TopQuery(testNamespace); got != "topk(10, count by (__name__) ("+want+"))" {
		t.Errorf("top query = %s", got)
	}
}

func TestUTF8NamespaceLabelIsQuoted(t *testing.T) {
	fake := testutil.NewFakePrometheus()
	defer fake.Close()
	p := newProvider(t, fake, "k8s.namespace")
	if got := p.Selector(testNamespace); !strings.HasPrefix(got, `{"k8s.namespace"="payments"`) {
		t.Errorf("selector = %s, want the label name quoted", got)
	}
}

func TestValidMeasurement(t *testing.T) {
	fake := testutil.NewFakePrometheus()
	defer fake.Close()
	p := newProvider(t, fake, "namespace")
	fake.SetResult(p.CountQuery(testNamespace), testutil.Sample{Value: 61230})
	fake.SetResult(p.TopQuery(testNamespace),
		testutil.Sample{Labels: map[string]string{"__name__": "small"}, Value: 10},
		testutil.Sample{Labels: map[string]string{"__name__": "http.server.duration"}, Value: 18400}, // UTF-8 name
	)

	m, err := p.Measure(context.Background(), testNamespace)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if m.ActiveSeries != 61230 {
		t.Errorf("ActiveSeries = %d", m.ActiveSeries)
	}
	if len(m.TopMetrics) != 2 || m.TopMetrics[0].Name != "http.server.duration" || m.TopMetrics[0].Series != 18400 {
		t.Errorf("TopMetrics = %+v, want largest first", m.TopMetrics)
	}
}

func TestInfosDoNotInvalidate(t *testing.T) {
	fake := testutil.NewFakePrometheus()
	defer fake.Close()
	p := newProvider(t, fake, "namespace")
	fake.SetResult(p.CountQuery(testNamespace), testutil.Sample{Value: 42})
	fake.SetInfos("PromQL info: metric might not be a counter")

	m, err := p.Measure(context.Background(), testNamespace)
	if err != nil || m.ActiveSeries != 42 {
		t.Errorf("m = %+v, err = %v; infos must not make the measurement invalid", m, err)
	}
}

func TestEmptyResultIsZeroWhenValid(t *testing.T) {
	fake := testutil.NewFakePrometheus()
	defer fake.Close()
	m, err := newProvider(t, fake, "namespace").Measure(context.Background(), testNamespace)
	if err != nil || m.ActiveSeries != 0 || len(m.TopMetrics) != 0 {
		t.Errorf("m = %+v, err = %v; want 0 series and no error", m, err)
	}
}

func TestInvalidMeasurements(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testutil.FakePrometheus)
		want  InvalidReason
	}{
		{"not ready", func(f *testutil.FakePrometheus) { f.SetReady(false) }, BackendWarmingUp},
		{"recent restart", func(f *testutil.FakePrometheus) {
			f.SetRuntimeInfo(time.Now().Add(-time.Minute), true, time.Now())
		}, BackendWarmingUp},
		{"warnings", func(f *testutil.FakePrometheus) { f.SetWarnings("partial response") }, PartialData},
		{"query error", func(f *testutil.FakePrometheus) {
			f.SetQueryError(http.StatusServiceUnavailable, "overloaded")
		}, BackendUnreachable},
		{"down", func(f *testutil.FakePrometheus) { f.Close() }, BackendUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := testutil.NewFakePrometheus()
			defer fake.Close()
			p := newProvider(t, fake, "namespace")
			tc.setup(fake)
			_, err := p.Measure(context.Background(), testNamespace)
			expectInvalid(t, err, tc.want)
		})
	}
}

func TestMetricSeries(t *testing.T) {
	fake := testutil.NewFakePrometheus()
	defer fake.Close()
	p := newProvider(t, fake, "namespace")
	q := p.MetricSeriesQuery(testNamespace, "http.server.duration")
	if !strings.Contains(q, `__name__="http.server.duration"`) {
		t.Errorf("query = %s, want a __name__ matcher", q)
	}
	fake.SetResult(q, testutil.Sample{Value: 42})
	if n, err := p.MetricSeries(context.Background(), testNamespace, "http.server.duration"); err != nil || n != 42 {
		t.Errorf("MetricSeries = %d, %v; want 42", n, err)
	}
	if n, err := p.MetricSeries(context.Background(), testNamespace, "gone"); err != nil || n != 0 {
		t.Errorf("MetricSeries of a dropped metric = %d, %v; want 0", n, err)
	}
}

func TestConfigReloadedSince(t *testing.T) {
	fake := testutil.NewFakePrometheus()
	defer fake.Close()
	p := newProvider(t, fake, "namespace")
	change := time.Date(2026, 10, 12, 13, 41, 20, 600_000_000, time.UTC)

	for _, tc := range []struct {
		name       string
		success    bool
		lastConfig time.Time
		want       bool
	}{
		{"reloaded later", true, change.Add(time.Minute), true},
		{"reloaded in the same second", true, change, true}, // reported as 13:41:20, change at 13:41:20.6
		{"not reloaded yet", true, change.Add(-time.Minute), false},
		{"reload failed", false, change.Add(time.Minute), false},
	} {
		fake.SetRuntimeInfo(time.Now().Add(-time.Hour), tc.success, tc.lastConfig)
		got, err := p.ConfigReloadedSince(context.Background(), change)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestAsInvalidTreatsUnknownErrorsAsUnreachable(t *testing.T) {
	if got := AsInvalid(errors.New("boom")); got.Reason != BackendUnreachable {
		t.Errorf("reason = %s", got.Reason)
	}
}
