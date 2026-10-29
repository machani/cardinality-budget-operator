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

package controller

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	cardinalityv1beta1 "github.com/machani/cardinality-budget-operator/api/v1beta1"
	"github.com/machani/cardinality-budget-operator/internal/usage"
)

// statusInput is everything computeStatus needs. Keeping it a pure function (no API calls, no clock)
// makes every phase transition testable without a cluster.
type statusInput struct {
	Budget            *cardinalityv1beta1.CardinalityBudget
	Measurement       usage.Measurement
	MeasureErr        error
	Now               time.Time
	MaxMeasurementAge time.Duration
}

// computeStatus derives the new status from the spec, the previous status and one measurement
// (HLD "Reconcile loop" steps 3, 4 and 6 for Observe and Warn).
func computeStatus(in statusInput) cardinalityv1beta1.CardinalityBudgetStatus {
	budget := in.Budget
	st := *budget.Status.DeepCopy()
	st.ObservedGeneration = budget.Generation

	protected, _, specErr := compileSpec(budget)

	if in.MeasureErr != nil {
		applyInvalidMeasurement(&st, usage.AsInvalid(in.MeasureErr), in.Now, in.MaxMeasurementAge, budget.Generation)
	} else {
		applyValidMeasurement(&st, budget, in.Measurement, budget.Generation)
		if specErr == nil && plansDrops(budget.Spec.Mode, st.Phase) {
			st.PlannedDrops = plannedDrops(budget, st.TopMetrics, protected)
		} else {
			st.PlannedDrops = nil
		}
	}

	// InvalidSpec wins over the measurement reasons: only a person can fix it (ADR 0012).
	if specErr != nil {
		st.PlannedDrops = nil
		setCondition(&st, cardinalityv1beta1.ConditionReady, metav1.ConditionFalse,
			cardinalityv1beta1.ReasonInvalidSpec, specErr.Error(), budget.Generation)
	}
	return st
}

// applyInvalidMeasurement keeps the last valid numbers, sets phase Unknown and explains why (ADR 0005).
// Nothing is planned or dropped while the measurement cannot be trusted.
func applyInvalidMeasurement(st *cardinalityv1beta1.CardinalityBudgetStatus, invalid *usage.InvalidError,
	now time.Time, maxAge time.Duration, generation int64) {
	st.Phase = cardinalityv1beta1.PhaseUnknown
	st.PlannedDrops = nil

	readyReason, readyMessage := string(invalid.Reason), invalid.Message
	// Stale: the numbers still shown in status are older than --max-measurement-age.
	if st.LastMeasured != nil && now.Sub(st.LastMeasured.Time) > maxAge {
		readyReason = cardinalityv1beta1.ReasonStale
		readyMessage = fmt.Sprintf("no valid measurement since %s (%s)",
			st.LastMeasured.UTC().Format(time.RFC3339), invalid.Error())
	}
	setCondition(st, cardinalityv1beta1.ConditionReady, metav1.ConditionFalse, readyReason, readyMessage, generation)

	// A backend that returned warnings answered and is ready; only its data is incomplete.
	if invalid.Reason == usage.PartialData {
		setCondition(st, cardinalityv1beta1.ConditionBackendReachable, metav1.ConditionTrue,
			cardinalityv1beta1.ReasonBackendReady, "Prometheus answered", generation)
	} else {
		setCondition(st, cardinalityv1beta1.ConditionBackendReachable, metav1.ConditionFalse,
			string(invalid.Reason), invalid.Message, generation)
	}
	// BudgetExceeded and exceededSince stay as they were: we do not know the current usage.
}

// applyValidMeasurement records the measurement and computes the phase (HLD step 4).
func applyValidMeasurement(st *cardinalityv1beta1.CardinalityBudgetStatus, budget *cardinalityv1beta1.CardinalityBudget,
	m usage.Measurement, generation int64) {
	series := m.ActiveSeries
	percent := usagePercent(series, budget.Spec.MaxActiveSeries)
	measured := metav1.NewTime(m.Time)

	st.ActiveSeries = &series
	st.UsagePercent = &percent
	st.LastMeasured = &measured
	st.TopMetrics = make([]cardinalityv1beta1.MetricUsage, 0, len(m.TopMetrics))
	for _, t := range m.TopMetrics {
		st.TopMetrics = append(st.TopMetrics, cardinalityv1beta1.MetricUsage{Name: t.Name, Series: t.Series})
	}
	st.Phase = phaseFor(percent, budget.Spec.WarnThresholdPercent)

	if percent >= 100 {
		if st.ExceededSince == nil {
			st.ExceededSince = &measured
		}
		setCondition(st, cardinalityv1beta1.ConditionBudgetExceeded, metav1.ConditionTrue,
			cardinalityv1beta1.ReasonOverBudget, usageMessage(series, budget.Spec.MaxActiveSeries, percent), generation)
	} else {
		st.ExceededSince = nil
		setCondition(st, cardinalityv1beta1.ConditionBudgetExceeded, metav1.ConditionFalse,
			cardinalityv1beta1.ReasonWithinBudget, usageMessage(series, budget.Spec.MaxActiveSeries, percent), generation)
	}
	setCondition(st, cardinalityv1beta1.ConditionReady, metav1.ConditionTrue,
		cardinalityv1beta1.ReasonMeasurementValid, "measurement is valid and fresh", generation)
	setCondition(st, cardinalityv1beta1.ConditionBackendReachable, metav1.ConditionTrue,
		cardinalityv1beta1.ReasonBackendReady, "Prometheus answered and is ready", generation)
}

// usagePercent is series as a whole percentage of the limit, rounded down.
func usagePercent(series, limit int64) int32 {
	if limit <= 0 {
		return 0
	}
	return int32(min(series*100/limit, math.MaxInt32))
}

// phaseFor maps usage to a phase. Enforcing (drops in place) arrives in v0.2.
func phaseFor(percent, warnThreshold int32) cardinalityv1beta1.BudgetPhase {
	switch {
	case percent >= 100:
		return cardinalityv1beta1.PhaseExceeded
	case percent >= warnThreshold:
		return cardinalityv1beta1.PhaseWarning
	default:
		return cardinalityv1beta1.PhaseHealthy
	}
}

// plansDrops says whether plannedDrops is filled. Observe never plans (ADR 0015); Warn and Enforce plan
// from the warn threshold on, so teams see what would be dropped before they reach 100%.
// TODO(machani): plan only at 100% (what Enforce would drop right now) instead of from the warn
// threshold? Current choice gives lead time; the alternative matches the "next step" wording exactly.
func plansDrops(mode cardinalityv1beta1.BudgetMode, phase cardinalityv1beta1.BudgetPhase) bool {
	return mode != cardinalityv1beta1.ModeObserve &&
		(phase == cardinalityv1beta1.PhaseWarning || phase == cardinalityv1beta1.PhaseExceeded)
}

// plannedDrops picks the metrics the next enforcement step would drop: the largest first, skipping
// protected metrics and recording-rule names (containing ':'), at most maxMetricsToDrop and never above
// the remaining room under maxTotalDrops (ADR 0006).
func plannedDrops(budget *cardinalityv1beta1.CardinalityBudget, top []cardinalityv1beta1.MetricUsage,
	protected []*regexp.Regexp) []string {
	enforcement := budget.Spec.Enforcement
	limit := max(0, min(int(enforcement.MaxMetricsToDrop), int(enforcement.MaxTotalDrops)-len(budget.Status.EnforcedDrops)))
	planned := make([]string, 0, limit)
	for _, metric := range top {
		if len(planned) >= limit {
			break
		}
		if strings.Contains(metric.Name, ":") || isProtected(metric.Name, protected) ||
			slices.ContainsFunc(budget.Status.EnforcedDrops, func(d cardinalityv1beta1.EnforcedDrop) bool {
				return d.Metric == metric.Name
			}) {
			continue
		}
		planned = append(planned, metric.Name)
	}
	return planned
}

// compileSpec checks the parts of the spec CEL cannot fully check and returns them ready to use. Any
// error is InvalidSpec: the budget freezes (no drops added or removed) until it is fixed (ADR 0012).
func compileSpec(budget *cardinalityv1beta1.CardinalityBudget) ([]*regexp.Regexp, labels.Selector, error) {
	protected, err := compileProtected(budget.Spec.Enforcement.ProtectedMetrics)
	if err != nil {
		return nil, nil, err
	}
	selector := labels.Everything()
	if s := budget.Spec.Enforcement.MonitorSelector; s != nil {
		if selector, err = metav1.LabelSelectorAsSelector(s); err != nil {
			return nil, nil, fmt.Errorf("monitorSelector is not valid: %w", err)
		}
	}
	return protected, selector, nil
}

// compileProtected compiles every protectedMetrics pattern, fully anchored like Prometheus (ADR 0012).
// Any invalid pattern is an error: skipping it would fail open.
func compileProtected(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for i, p := range patterns {
		re, err := regexp.Compile("^(?:" + p + ")$")
		if err != nil {
			return nil, fmt.Errorf("protectedMetrics[%d] %q is not a valid regular expression: %w", i, p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func isProtected(name string, protected []*regexp.Regexp) bool {
	return slices.ContainsFunc(protected, func(re *regexp.Regexp) bool { return re.MatchString(name) })
}

func usageMessage(series, limit int64, percent int32) string {
	return fmt.Sprintf("%d of %d series (%d%%)", series, limit, percent)
}

func setCondition(st *cardinalityv1beta1.CardinalityBudgetStatus, conditionType string,
	status metav1.ConditionStatus, reason, message string, generation int64) {
	meta.SetStatusCondition(&st.Conditions, metav1.Condition{
		Type: conditionType, Status: status, Reason: reason, Message: message, ObservedGeneration: generation,
	})
}

// readyReason returns the reason of the Ready condition, or "" when it is not set.
func readyReason(st *cardinalityv1beta1.CardinalityBudgetStatus) string {
	if c := meta.FindStatusCondition(st.Conditions, cardinalityv1beta1.ConditionReady); c != nil {
		return c.Reason
	}
	return ""
}
