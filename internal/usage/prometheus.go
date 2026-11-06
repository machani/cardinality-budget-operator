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
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// userAgentName identifies the operator in backend requests.
const userAgentName = "cardinality-budget-operator"

// Settings configure the Prometheus provider. They come from the validated operator flags.
type Settings struct {
	// URL is the Prometheus base URL (no credentials).
	URL string
	// HTTPConfig holds auth and TLS; nil means plain HTTP(S) with defaults.
	HTTPConfig *commonconfig.HTTPClientConfig
	// NamespaceLabel attributes a series to a namespace (ADR 0003).
	NamespaceLabel string
	// ExcludeMatchers remove cluster exporters and recording-rule outputs (ADR 0013).
	ExcludeMatchers []*labels.Matcher
	// QueryTimeout bounds every request.
	QueryTimeout time.Duration
	// BackendWarmup is the minimum backend uptime for a valid measurement (ADR 0005).
	BackendWarmup time.Duration
	// Now returns the current time; nil means time.Now. Tests replace it.
	Now func() time.Time
}

// PrometheusProvider measures usage through the Prometheus 3.x HTTP API.
type PrometheusProvider struct {
	settings Settings
	api      promv1.API
	http     *http.Client
	baseURL  string
}

var _ Provider = (*PrometheusProvider)(nil)

// NewPrometheusProvider builds the provider. It does not contact Prometheus.
func NewPrometheusProvider(s Settings) (*PrometheusProvider, error) {
	httpConfig := commonconfig.DefaultHTTPClientConfig
	if s.HTTPConfig != nil {
		httpConfig = *s.HTTPConfig
	}
	rt, err := commonconfig.NewRoundTripperFromConfig(httpConfig, userAgentName)
	if err != nil {
		return nil, fmt.Errorf("building the Prometheus HTTP client: %w", err)
	}
	client, err := api.NewClient(api.Config{Address: s.URL, RoundTripper: rt})
	if err != nil {
		return nil, fmt.Errorf("building the Prometheus API client: %w", err)
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	return &PrometheusProvider{
		settings: s,
		api:      promv1.NewAPI(client),
		http:     &http.Client{Transport: rt, Timeout: s.QueryTimeout},
		baseURL:  strings.TrimSuffix(s.URL, "/"),
	}, nil
}

// Selector returns the series selector for a namespace: the namespace matcher, any extra matchers, then
// the exclusions. Label names that are not valid legacy names are quoted, so Prometheus 3 UTF-8 names work.
func (p *PrometheusProvider) Selector(namespace string, extra ...*labels.Matcher) string {
	matchers := make([]string, 0, 1+len(extra)+len(p.settings.ExcludeMatchers))
	matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, p.settings.NamespaceLabel, namespace).String())
	for _, m := range extra {
		matchers = append(matchers, m.String())
	}
	for _, m := range p.settings.ExcludeMatchers {
		matchers = append(matchers, m.String())
	}
	return "{" + strings.Join(matchers, ",") + "}"
}

// MetricSeriesQuery returns the query for one metric's series in the namespace.
func (p *PrometheusProvider) MetricSeriesQuery(namespace, metric string) string {
	return "count(" + p.Selector(namespace, labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, metric)) + ")"
}

// MetricSeries returns one metric's series in the namespace; an empty result is 0.
func (p *PrometheusProvider) MetricSeries(ctx context.Context, namespace, metric string) (int64, error) {
	vector, err := p.query(ctx, p.MetricSeriesQuery(namespace, metric), p.settings.Now())
	if err != nil {
		return 0, err
	}
	if len(vector) == 0 {
		return 0, nil
	}
	return int64(vector[0].Value), nil
}

// ConfigReloadedSince checks runtimeinfo: the last reload succeeded and happened at or after t.
// Prometheus reports lastConfigTime in whole seconds, so t is truncated to the second first; otherwise a
// reload in the same second as the change would look older than it (Phase 1 fact 5).
func (p *PrometheusProvider) ConfigReloadedSince(ctx context.Context, t time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, p.settings.QueryTimeout)
	defer cancel()
	info, err := p.api.Runtimeinfo(ctx)
	if err != nil {
		return false, &InvalidError{Reason: BackendUnreachable, Message: "runtimeinfo failed: " + err.Error()}
	}
	return info.ReloadConfigSuccess && !info.LastConfigTime.Before(t.Truncate(time.Second)), nil
}

// CountQuery and TopQuery are the two usage queries (CLAUDE.md "Reconcile rules" step 3).
func (p *PrometheusProvider) CountQuery(namespace string) string {
	return "count(" + p.Selector(namespace) + ")"
}

// TopQuery returns the query for the TopMetricsLimit metrics with the most series.
func (p *PrometheusProvider) TopQuery(namespace string) string {
	return fmt.Sprintf("topk(%d, count by (__name__) (%s))", TopMetricsLimit, p.Selector(namespace))
}

// Measure runs the validity checks and the usage queries (ADR 0005). Order matters: a backend that is
// not ready or still warming up gives falsely low counts, so it is checked before querying.
func (p *PrometheusProvider) Measure(ctx context.Context, namespace string) (Measurement, error) {
	if err := p.checkReady(ctx); err != nil {
		return Measurement{}, err
	}
	if err := p.checkWarmup(ctx); err != nil {
		return Measurement{}, err
	}

	now := p.settings.Now()
	count, err := p.query(ctx, p.CountQuery(namespace), now)
	if err != nil {
		return Measurement{}, err
	}
	top, err := p.query(ctx, p.TopQuery(namespace), now)
	if err != nil {
		return Measurement{}, err
	}

	m := Measurement{Time: now, TopMetrics: make([]MetricCount, 0, len(top))}
	// count() over no series returns an empty vector; that is 0 here because every check passed.
	if len(count) > 0 {
		m.ActiveSeries = int64(count[0].Value)
	}
	for _, s := range top {
		m.TopMetrics = append(m.TopMetrics, MetricCount{Name: string(s.Metric[model.MetricNameLabel]), Series: int64(s.Value)})
	}
	slices.SortFunc(m.TopMetrics, func(a, b MetricCount) int {
		return cmp.Or(cmp.Compare(b.Series, a.Series), cmp.Compare(a.Name, b.Name))
	})
	if len(m.TopMetrics) > TopMetricsLimit {
		m.TopMetrics = m.TopMetrics[:TopMetricsLimit]
	}
	return m, nil
}

// checkReady calls /-/ready. Not answering means unreachable; answering "not ready" means Prometheus is
// still starting (replaying its WAL), which counts as warming up.
func (p *PrometheusProvider) checkReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.settings.QueryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/-/ready", nil)
	if err != nil {
		return &InvalidError{Reason: BackendUnreachable, Message: "building the readiness request: " + err.Error()}
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return &InvalidError{Reason: BackendUnreachable, Message: "readiness check failed: " + err.Error()}
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusServiceUnavailable:
		return &InvalidError{Reason: BackendWarmingUp, Message: "Prometheus reports not ready"}
	default:
		return &InvalidError{Reason: BackendUnreachable, Message: fmt.Sprintf("readiness check returned HTTP %d", resp.StatusCode)}
	}
}

// checkWarmup requires the backend to have run for at least --backend-warmup: right after a restart the
// counts are falsely low, and low counts would trigger restores (ADR 0005).
func (p *PrometheusProvider) checkWarmup(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.settings.QueryTimeout)
	defer cancel()
	info, err := p.api.Runtimeinfo(ctx)
	if err != nil {
		return &InvalidError{Reason: BackendUnreachable, Message: "runtimeinfo failed: " + err.Error()}
	}
	if uptime := p.settings.Now().Sub(info.StartTime); uptime < p.settings.BackendWarmup {
		return &InvalidError{Reason: BackendWarmingUp,
			Message: fmt.Sprintf("Prometheus started %s ago, less than --backend-warmup %s",
				uptime.Truncate(time.Second), p.settings.BackendWarmup)}
	}
	return nil
}

// query runs an instant query. Any warning makes the result partial and therefore invalid (ADR 0005).
// Infos (Prometheus 3 informational annotations, for example "metric might not be a counter") say
// nothing about completeness, so they are only logged.
func (p *PrometheusProvider) query(ctx context.Context, q string, ts time.Time) (model.Vector, error) {
	value, warnings, infos, err := p.api.Query(ctx, q, ts, promv1.WithTimeout(p.settings.QueryTimeout))
	if err != nil {
		return nil, &InvalidError{Reason: BackendUnreachable, Message: "query failed: " + err.Error()}
	}
	if len(warnings) > 0 {
		return nil, &InvalidError{Reason: PartialData, Message: "query returned warnings: " + strings.Join(warnings, "; ")}
	}
	if len(infos) > 0 {
		logf.FromContext(ctx).V(1).Info("query returned infos", "infos", strings.Join(infos, "; "))
	}
	if value == nil {
		return nil, &InvalidError{Reason: BackendUnreachable, Message: "query returned no result"}
	}
	vector, ok := value.(model.Vector)
	if !ok {
		return nil, &InvalidError{Reason: BackendUnreachable, Message: "unexpected result type " + value.Type().String()}
	}
	return vector, nil
}
