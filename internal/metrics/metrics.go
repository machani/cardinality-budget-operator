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

// Package metrics holds the operator's own metrics: one series per budget and a few cluster gauges
// (CLAUDE.md "Coding conventions"). Metric names and other unbounded values are never used as labels.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// budgetNamespaceLabel names the budget's namespace. It is not called "namespace", because Prometheus
// sets that label from the scrape target and would rename ours to "exported_namespace".
const budgetNamespaceLabel = "budget_namespace"

var (
	activeSeries = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cardinalitybudget_active_series",
		Help: "Active series in the budget's namespace at the last valid measurement.",
	}, []string{budgetNamespaceLabel})
	limit = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cardinalitybudget_limit",
		Help: "The budget's maxActiveSeries.",
	}, []string{budgetNamespaceLabel})
	usageRatio = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cardinalitybudget_usage_ratio",
		Help: "Active series divided by maxActiveSeries (1 = 100%).",
	}, []string{budgetNamespaceLabel})
	dropsActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cardinalitybudget_drops_active",
		Help: "Metrics currently dropped by the budget.",
	}, []string{budgetNamespaceLabel})
	measurementValid = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cardinalitybudget_measurement_valid",
		Help: "1 if the last measurement was valid and fresh (Ready=True), 0 otherwise.",
	}, []string{budgetNamespaceLabel})
	// budgetPaused has one series per paused budget. The reason label is bounded (Ineffective,
	// ConfigReloadFailed, Reverted), so it is safe as a label.
	budgetPaused = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cardinalitybudget_paused",
		Help: "1 while the budget is paused until a person resumes it, by reason (ADR 0008).",
	}, []string{budgetNamespaceLabel, "reason"})
	enforcementPaused = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cardinalitybudget_enforcement_paused",
		Help: "1 if the global kill switch --enforcement-paused is on (ADR 0010).",
	})
	dropsLastHour = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cardinalitybudget_drops_last_hour",
		Help: "New drops across all budgets in the last hour, as counted for --max-drops-per-hour (ADR 0010).",
	})
)

func init() {
	ctrlmetrics.Registry.MustRegister(activeSeries, limit, usageRatio, dropsActive, measurementValid,
		budgetPaused, enforcementPaused, dropsLastHour)
}

// BudgetState is what the metrics show for one budget.
type BudgetState struct {
	Limit int64
	// ActiveSeries is nil until the first valid measurement.
	ActiveSeries *int64
	DropsActive  int
	Ready        bool
	// PauseReason is the Enforced reason that pauses the budget, or "" when it is not paused.
	PauseReason string
}

// SetBudget updates the series of one budget.
func SetBudget(namespace string, s BudgetState) {
	limit.WithLabelValues(namespace).Set(float64(s.Limit))
	dropsActive.WithLabelValues(namespace).Set(float64(s.DropsActive))
	measurementValid.WithLabelValues(namespace).Set(boolToFloat(s.Ready))
	budgetPaused.DeletePartialMatch(prometheus.Labels{budgetNamespaceLabel: namespace})
	if s.PauseReason != "" {
		budgetPaused.WithLabelValues(namespace, s.PauseReason).Set(1)
	}
	if s.ActiveSeries != nil {
		activeSeries.WithLabelValues(namespace).Set(float64(*s.ActiveSeries))
		if s.Limit > 0 {
			usageRatio.WithLabelValues(namespace).Set(float64(*s.ActiveSeries) / float64(s.Limit))
		}
	}
}

// DeleteBudget removes every series of a deleted budget, so its last values do not linger.
func DeleteBudget(namespace string) {
	for _, g := range []*prometheus.GaugeVec{activeSeries, limit, usageRatio, dropsActive, measurementValid} {
		g.DeleteLabelValues(namespace)
	}
	budgetPaused.DeletePartialMatch(prometheus.Labels{budgetNamespaceLabel: namespace})
}

// SetEnforcementPaused reports the kill switch.
func SetEnforcementPaused(paused bool) { enforcementPaused.Set(boolToFloat(paused)) }

// SetDropsLastHour reports the drops counted for the hourly limit.
func SetDropsLastHour(n int) { dropsLastHour.Set(float64(n)) }

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
