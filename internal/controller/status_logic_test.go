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
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
	"github.com/machani/cardinality-budget-operator/internal/usage"
)

// Plain unit tests for computeStatus and its helpers: no API server needed.

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func testBudget(mode cardinalityv1alpha1.BudgetMode) *cardinalityv1alpha1.CardinalityBudget {
	return &cardinalityv1alpha1.CardinalityBudget{
		ObjectMeta: metav1.ObjectMeta{Name: budgetName, Namespace: "payments", Generation: 3},
		Spec: cardinalityv1alpha1.CardinalityBudgetSpec{
			MaxActiveSeries: 1000, WarnThresholdPercent: 90, Mode: mode,
			Enforcement: cardinalityv1alpha1.EnforcementSpec{MaxMetricsToDrop: 2, MaxTotalDrops: 5},
		},
	}
}

func measured(series int64, at time.Time, top ...usage.MetricCount) statusInput {
	return statusInput{Measurement: usage.Measurement{ActiveSeries: series, TopMetrics: top, Time: at},
		Now: at, MaxMeasurementAge: 10 * time.Minute}
}

func invalidAt(reason usage.InvalidReason, at time.Time) statusInput {
	return statusInput{MeasureErr: &usage.InvalidError{Reason: reason, Message: "test"}, Now: at,
		MaxMeasurementAge: 10 * time.Minute}
}

// step applies one measurement to the budget, like one reconcile, and returns the new status.
func step(b *cardinalityv1alpha1.CardinalityBudget, in statusInput) cardinalityv1alpha1.CardinalityBudgetStatus {
	in.Budget = b
	b.Status = computeStatus(in)
	return b.Status
}

func condition(st cardinalityv1alpha1.CardinalityBudgetStatus, t string) *metav1.Condition {
	return meta.FindStatusCondition(st.Conditions, t)
}

func TestPhaseTransitions(t *testing.T) {
	b := testBudget(cardinalityv1alpha1.ModeObserve)
	for _, tc := range []struct {
		series        int64
		phase         cardinalityv1alpha1.BudgetPhase
		exceeded      bool
		exceededSince bool
	}{
		{500, cardinalityv1alpha1.PhaseHealthy, false, false},
		{899, cardinalityv1alpha1.PhaseHealthy, false, false}, // 89.9% rounds down to 89
		{900, cardinalityv1alpha1.PhaseWarning, false, false},
		{1000, cardinalityv1alpha1.PhaseExceeded, true, true},
		{1500, cardinalityv1alpha1.PhaseExceeded, true, true},
		{800, cardinalityv1alpha1.PhaseHealthy, false, false},
	} {
		st := step(b, measured(tc.series, t0))
		if st.Phase != tc.phase {
			t.Errorf("%d series: phase %s, want %s", tc.series, st.Phase, tc.phase)
		}
		if got := condition(st, cardinalityv1alpha1.ConditionBudgetExceeded).Status == metav1.ConditionTrue; got != tc.exceeded {
			t.Errorf("%d series: BudgetExceeded %v, want %v", tc.series, got, tc.exceeded)
		}
		if (st.ExceededSince != nil) != tc.exceededSince {
			t.Errorf("%d series: exceededSince %v", tc.series, st.ExceededSince)
		}
		if condition(st, cardinalityv1alpha1.ConditionReady).Reason != cardinalityv1alpha1.ReasonMeasurementValid {
			t.Errorf("%d series: Ready reason %s", tc.series, condition(st, cardinalityv1alpha1.ConditionReady).Reason)
		}
		if st.ObservedGeneration != 3 {
			t.Errorf("observedGeneration = %d", st.ObservedGeneration)
		}
	}
}

func TestExceededSinceKeepsTheFirstTime(t *testing.T) {
	b := testBudget(cardinalityv1alpha1.ModeObserve)
	step(b, measured(1200, t0))
	st := step(b, measured(1300, t0.Add(5*time.Minute)))
	if !st.ExceededSince.Time.Equal(t0) {
		t.Errorf("exceededSince = %v, want the first time over budget %v", st.ExceededSince, t0)
	}
}

func TestEmptyNamespaceIsZeroNotNil(t *testing.T) {
	st := step(testBudget(cardinalityv1alpha1.ModeObserve), measured(0, t0))
	if st.ActiveSeries == nil || *st.ActiveSeries != 0 || st.Phase != cardinalityv1alpha1.PhaseHealthy {
		t.Errorf("status = %+v, want 0 series and Healthy", st)
	}
}

func TestInvalidMeasurementKeepsNumbersAndSetsUnknown(t *testing.T) {
	for _, tc := range []struct {
		reason    usage.InvalidReason
		reachable metav1.ConditionStatus
	}{
		{usage.BackendUnreachable, metav1.ConditionFalse},
		{usage.BackendWarmingUp, metav1.ConditionFalse},
		{usage.PartialData, metav1.ConditionTrue},
	} {
		b := testBudget(cardinalityv1alpha1.ModeWarn)
		step(b, measured(1200, t0, usage.MetricCount{Name: "big", Series: 900}))
		st := step(b, invalidAt(tc.reason, t0.Add(5*time.Minute)))

		if st.Phase != cardinalityv1alpha1.PhaseUnknown || *st.ActiveSeries != 1200 || !st.LastMeasured.Time.Equal(t0) {
			t.Errorf("%s: phase %s, series %d, lastMeasured %v; want Unknown with the last valid numbers",
				tc.reason, st.Phase, *st.ActiveSeries, st.LastMeasured)
		}
		if len(st.PlannedDrops) != 0 {
			t.Errorf("%s: plannedDrops %v, want none while the measurement is invalid", tc.reason, st.PlannedDrops)
		}
		if r := condition(st, cardinalityv1alpha1.ConditionReady); r.Status != metav1.ConditionFalse || r.Reason != string(tc.reason) {
			t.Errorf("%s: Ready %s/%s", tc.reason, r.Status, r.Reason)
		}
		if got := condition(st, cardinalityv1alpha1.ConditionBackendReachable).Status; got != tc.reachable {
			t.Errorf("%s: BackendReachable %s, want %s", tc.reason, got, tc.reachable)
		}
		if st.ExceededSince == nil || condition(st, cardinalityv1alpha1.ConditionBudgetExceeded).Status != metav1.ConditionTrue {
			t.Errorf("%s: exceeded state must stay as it was", tc.reason)
		}
	}
}

func TestStaleAfterMaxMeasurementAge(t *testing.T) {
	b := testBudget(cardinalityv1alpha1.ModeObserve)
	step(b, measured(100, t0))
	st := step(b, invalidAt(usage.BackendUnreachable, t0.Add(11*time.Minute)))
	if r := condition(st, cardinalityv1alpha1.ConditionReady); r.Reason != cardinalityv1alpha1.ReasonStale {
		t.Errorf("Ready reason = %s, want Stale", r.Reason)
	}
	if r := condition(st, cardinalityv1alpha1.ConditionBackendReachable); r.Reason != cardinalityv1alpha1.ReasonBackendUnreachable {
		t.Errorf("BackendReachable reason = %s, want the underlying cause", r.Reason)
	}
}

func TestNeverMeasuredIsNotStale(t *testing.T) {
	st := step(testBudget(cardinalityv1alpha1.ModeObserve), invalidAt(usage.BackendWarmingUp, t0))
	if st.ActiveSeries != nil || condition(st, cardinalityv1alpha1.ConditionReady).Reason != cardinalityv1alpha1.ReasonBackendWarmingUp {
		t.Errorf("status = %+v, want no numbers and reason BackendWarmingUp", st)
	}
}

func TestPlannedDrops(t *testing.T) {
	top := []usage.MetricCount{
		{Name: "up", Series: 900},                        // protected (exact, anchored)
		{Name: "namespace:requests:rate5m", Series: 800}, // recording rule
		{Name: "http_requests_total", Series: 700},
		{Name: "slo_latency_bucket", Series: 600}, // protected by slo_.*
		{Name: "uptime_seconds", Series: 500},     // not protected: "up" is anchored
		{Name: "rpc_calls", Series: 400},
	}
	b := testBudget(cardinalityv1alpha1.ModeWarn)
	b.Spec.Enforcement.ProtectedMetrics = []string{"up", "slo_.*"}

	st := step(b, measured(1200, t0, top...))
	if want := []string{"http_requests_total", "uptime_seconds"}; !slices.Equal(st.PlannedDrops, want) {
		t.Errorf("plannedDrops = %v, want %v (largest first, filtered, at most maxMetricsToDrop)", st.PlannedDrops, want)
	}

	b.Status.EnforcedDrops = []cardinalityv1alpha1.EnforcedDrop{
		{Metric: "a"}, {Metric: "b"}, {Metric: "c"}, {Metric: "http_requests_total"},
	}
	st = step(b, measured(1200, t0, top...))
	if want := []string{"uptime_seconds"}; !slices.Equal(st.PlannedDrops, want) {
		t.Errorf("plannedDrops = %v, want %v (room under maxTotalDrops, skip already dropped)", st.PlannedDrops, want)
	}
}

func TestPlannedDropsByModeAndPhase(t *testing.T) {
	top := []usage.MetricCount{{Name: "big", Series: 900}}
	for _, tc := range []struct {
		mode    cardinalityv1alpha1.BudgetMode
		series  int64
		planned bool
	}{
		{cardinalityv1alpha1.ModeObserve, 1200, false},
		{cardinalityv1alpha1.ModeWarn, 500, false},
		{cardinalityv1alpha1.ModeWarn, 950, true},
		{cardinalityv1alpha1.ModeWarn, 1200, true},
		{cardinalityv1alpha1.ModeEnforce, 1200, true},
	} {
		st := step(testBudget(tc.mode), measured(tc.series, t0, top...))
		if got := len(st.PlannedDrops) > 0; got != tc.planned {
			t.Errorf("%s at %d series: planned %v, want %v", tc.mode, tc.series, st.PlannedDrops, tc.planned)
		}
	}
}

func TestInvalidProtectedPatternFreezesPlanning(t *testing.T) {
	b := testBudget(cardinalityv1alpha1.ModeWarn)
	b.Spec.Enforcement.ProtectedMetrics = []string{"slo_(.*"}
	st := step(b, measured(1200, t0, usage.MetricCount{Name: "slo_latency", Series: 900}))

	if r := condition(st, cardinalityv1alpha1.ConditionReady); r.Status != metav1.ConditionFalse || r.Reason != cardinalityv1alpha1.ReasonInvalidSpec {
		t.Errorf("Ready = %s/%s, want False/InvalidSpec", r.Status, r.Reason)
	}
	if len(st.PlannedDrops) != 0 {
		t.Errorf("plannedDrops = %v, want none (fail closed)", st.PlannedDrops)
	}
	if st.Phase != cardinalityv1alpha1.PhaseExceeded || *st.ActiveSeries != 1200 {
		t.Errorf("phase %s and usage must still update", st.Phase)
	}
	// InvalidSpec also wins over an invalid measurement.
	st = step(b, invalidAt(usage.BackendUnreachable, t0.Add(time.Minute)))
	if condition(st, cardinalityv1alpha1.ConditionReady).Reason != cardinalityv1alpha1.ReasonInvalidSpec {
		t.Errorf("InvalidSpec must take precedence")
	}
}

func TestStateEvents(t *testing.T) {
	b := testBudget(cardinalityv1alpha1.ModeObserve)
	var events []string
	record := func(in statusInput) {
		old := b.Status.DeepCopy()
		st := step(b, in)
		if e := stateEvent(old, &st); e != nil {
			events = append(events, e.Type+"/"+e.Reason)
		}
	}
	record(measured(100, t0))                                          // first measurement: Healthy
	record(measured(120, t0.Add(time.Minute)))                         // unchanged state: no Event
	record(measured(950, t0.Add(2*time.Minute)))                       // Warning
	record(invalidAt(usage.BackendWarmingUp, t0.Add(3*time.Minute)))   // Unknown, warming up
	record(invalidAt(usage.BackendUnreachable, t0.Add(4*time.Minute))) // same phase, new reason
	record(invalidAt(usage.BackendUnreachable, t0.Add(5*time.Minute))) // unchanged: no Event
	record(measured(100, t0.Add(6*time.Minute)))                       // back to Healthy

	want := []string{"Normal/BudgetHealthy", "Warning/BudgetWarning", "Warning/BackendWarmingUp",
		"Warning/BackendUnreachable", "Normal/BudgetHealthy"}
	if !slices.Equal(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
}

func TestMergeEventsCombinesSameReason(t *testing.T) {
	got := mergeEvents([]budgetEvent{
		{Type: "Warning", Reason: "Reverted", Note: "a removed"},
		{Type: "Warning", Reason: "BudgetExceeded", Note: "over"},
		{Type: "Warning", Reason: "Reverted", Note: "b removed"},
		{Type: "Normal", Reason: "Reverted", Note: "other type"},
	})
	want := []budgetEvent{
		{Type: "Warning", Reason: "Reverted", Note: "a removed; b removed"},
		{Type: "Warning", Reason: "BudgetExceeded", Note: "over"},
		{Type: "Normal", Reason: "Reverted", Note: "other type"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEvents = %v, want %v", got, want)
	}
}

func TestMergeEventsKeepsNoteWithinLimit(t *testing.T) {
	long := strings.Repeat("é", maxEventNote) // 2 bytes each
	got := mergeEvents([]budgetEvent{{Type: "Warning", Reason: "Dropped", Note: long}})[0].Note
	if len(got) > maxEventNote || !utf8.ValidString(got) || !strings.HasSuffix(got, " …") {
		t.Errorf("note of %d bytes, valid UTF-8 %v, want at most %d bytes ending in an ellipsis",
			len(got), utf8.ValidString(got), maxEventNote)
	}
}

func TestJitterStaysWithinTenPercent(t *testing.T) {
	for range 1000 {
		if d := jitter(5 * time.Minute); d < 270*time.Second || d > 330*time.Second {
			t.Fatalf("jitter = %s, want 4m30s..5m30s", d)
		}
	}
}

func TestUsagePercent(t *testing.T) {
	if got := usagePercent(61230, 50000); got != 122 {
		t.Errorf("usagePercent = %d, want 122", got)
	}
}
