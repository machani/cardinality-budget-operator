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
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
	"github.com/machani/cardinality-budget-operator/internal/enforce"
	"github.com/machani/cardinality-budget-operator/internal/metrics"
)

// reloadTimeout is how long Prometheus may take to load a changed config before the budget pauses
// with ConfigReloadFailed (ADR 0006).
const reloadTimeout = 5 * time.Minute

// effectiveFraction: a drop is Effective when at most this share of its series remains (90% rule, ADR 0006).
const effectiveFraction = 0.10

// errSkipEnforcement means the backend could not answer an enforcement question (reload or series
// check). Like an invalid measurement, nothing is added or removed this time (ADR 0005).
var errSkipEnforcement = errors.New("backend could not confirm the state; enforcement skipped")

// enforcement runs HLD reconcile steps 5 to 7 for one budget whose measurement is valid and whose spec is
// usable. It changes monitors through the Writer and updates budget.Status in memory; the caller writes
// the status. Order of safety checks follows CLAUDE.md "Safety".
type enforcement struct {
	r             *CardinalityBudgetReconciler
	log           logr.Logger
	budget        *cardinalityv1alpha1.CardinalityBudget
	oldGeneration int64 // status.observedGeneration before this reconcile (ADR 0009 re-sync)
	now           time.Time
	writer        *enforce.Writer
	protected     []*regexp.Regexp

	// monitors holds the latest copy of every relevant monitor by UID; selected marks the ones the
	// monitorSelector picks. Copies are refreshed after each write.
	monitors map[types.UID]enforce.Monitor
	selected map[types.UID]bool
	order    []types.UID // stable iteration order

	events        []budgetEvent
	rulesChanged  bool   // a write changed metricRelabelings: Prometheus must reload, a new settle period starts
	limitReached  bool   // over budget with no room or no candidate
	rateLimited   bool   // the global hourly drop limit is reached (ADR 0010)
	newPause      string // pause reason raised in this reconcile
	pauseMessages map[string]string
	resync        map[string]bool // adopted drops to extend to every selected monitor
}

func newEnforcement(r *CardinalityBudgetReconciler, log logr.Logger, budget *cardinalityv1alpha1.CardinalityBudget,
	oldGeneration int64, now time.Time, protected []*regexp.Regexp) *enforcement {
	return &enforcement{
		r: r, log: log, budget: budget, oldGeneration: oldGeneration, now: now, protected: protected,
		writer:        &enforce.Writer{Client: r.Client, Paused: r.EnforcementPaused},
		pauseMessages: map[string]string{},
		resync:        map[string]bool{},
	}
}

// run executes the step and returns how soon to reconcile again (0 means the normal interval). Events to
// record are collected in e.events.
func (e *enforcement) run(ctx context.Context, selector labels.Selector) (time.Duration, error) {
	st := &e.budget.Status
	if err := e.loadMonitors(ctx, selector); err != nil {
		return 0, err
	}

	// Reload check and settle period (ADR 0006).
	reloaded := true
	if st.LastEnforcementTime != nil {
		ok, err := e.r.Provider.ConfigReloadedSince(ctx, st.LastEnforcementTime.Time)
		if err != nil {
			return 0, errSkipEnforcement
		}
		reloaded = ok
		unverified := e.now.Before(st.LastEnforcementTime.Add(e.r.SettlePeriod)) || hasPending(st.EnforcedDrops)
		if !reloaded && unverified && e.now.Sub(st.LastEnforcementTime.Time) > reloadTimeout {
			e.pause(cardinalityv1alpha1.ReasonConfigReloadFailed,
				fmt.Sprintf("Prometheus did not load its configuration within %s of the last change", reloadTimeout))
		}
	}
	settled := st.LastEnforcementTime == nil ||
		(reloaded && !e.now.Before(st.LastEnforcementTime.Add(e.r.SettlePeriod)))

	if err := e.checkEffectiveness(ctx, reloaded); err != nil {
		return 0, err
	}
	if err := e.cleanUpReverted(ctx); err != nil {
		return 0, err
	}
	if err := e.adoptUnrecorded(ctx); err != nil {
		return 0, err
	}
	if err := e.cleanDeselected(ctx); err != nil {
		return 0, err
	}
	if err := e.restoreProtected(ctx); err != nil {
		return 0, err
	}
	if err := e.checkDriftAndExtend(ctx); err != nil {
		return 0, err
	}
	if e.rulesChanged || e.youngestPendingLeft() > 0 {
		// A rule change earlier in this run (an immediate restore, rules left over after a resume) is this
		// run's step, and a Pending drop younger than the settle period (for example one adopted after a lost
		// status write) is still being checked: no drop or restore follows at once (ADR 0006, ADR 0016).
		// Changes that touch only annotations or status do not count: Prometheus sees nothing to reload.
		settled = false
	}
	if err := e.act(ctx, settled); err != nil {
		return 0, err
	}

	if e.rulesChanged {
		now := metav1.NewTime(e.now)
		st.LastEnforcementTime = &now
	}
	e.finishStatus()

	left := e.youngestPendingLeft()
	if st.LastEnforcementTime != nil {
		left = max(left, st.LastEnforcementTime.Add(e.r.SettlePeriod).Sub(e.now))
	}
	if left > 0 {
		return left + time.Second, nil
	}
	return 0, nil
}

// youngestPendingLeft is how long until every Pending drop is older than the settle period (0 if none is).
func (e *enforcement) youngestPendingLeft() time.Duration {
	var left time.Duration
	for _, d := range e.budget.Status.EnforcedDrops {
		if d.State == cardinalityv1alpha1.DropPending {
			left = max(left, d.Since.Add(e.r.SettlePeriod).Sub(e.now))
		}
	}
	return left
}

// loadMonitors reads the selected monitors and every monitor carrying the enforced label.
func (e *enforcement) loadMonitors(ctx context.Context, selector labels.Selector) error {
	ns := e.budget.Namespace
	selected, err := enforce.List(ctx, e.r.Client, ns, selector)
	if err != nil {
		return fmt.Errorf("listing monitors: %w", err)
	}
	owned, err := enforce.ListOwned(ctx, e.r.Client, ns)
	if err != nil {
		return fmt.Errorf("listing enforced monitors: %w", err)
	}
	e.monitors, e.selected = map[types.UID]enforce.Monitor{}, map[types.UID]bool{}
	for _, m := range selected {
		e.add(m)
		e.selected[m.Object.GetUID()] = true
	}
	for _, m := range owned {
		e.add(m)
	}
	return nil
}

func (e *enforcement) add(m enforce.Monitor) {
	uid := m.Object.GetUID()
	if _, ok := e.monitors[uid]; !ok {
		e.order = append(e.order, uid)
	}
	e.monitors[uid] = m
}

// write sends one monitor change and keeps the fresh copy. A monitor deleted meanwhile is fine.
func (e *enforcement) write(ctx context.Context, uid types.UID, updated enforce.Monitor) error {
	if err := e.writer.Patch(ctx, e.monitors[uid], updated); err != nil {
		if apierrors.IsNotFound(err) {
			delete(e.monitors, uid)
			return nil
		}
		return fmt.Errorf("writing %s: %w", updated.Key(), err)
	}
	if !enforce.SameRules(e.monitors[uid], updated) {
		e.rulesChanged = true
	}
	e.monitors[uid] = updated
	return nil
}

// checkEffectiveness marks Pending drops Effective or Ineffective once the settle period has passed and
// Prometheus has loaded the change (ADR 0006). An Ineffective drop keeps its rule and pauses the budget.
func (e *enforcement) checkEffectiveness(ctx context.Context, reloaded bool) error {
	for i := range e.budget.Status.EnforcedDrops {
		d := &e.budget.Status.EnforcedDrops[i]
		if d.State != cardinalityv1alpha1.DropPending || !reloaded || e.now.Before(d.Since.Add(e.r.SettlePeriod)) {
			continue
		}
		series, err := e.r.Provider.MetricSeries(ctx, e.budget.Namespace, d.Metric)
		if err != nil {
			return errSkipEnforcement
		}
		if float64(series) <= effectiveFraction*float64(d.SeriesAtDrop) {
			d.State = cardinalityv1alpha1.DropEffective
			continue
		}
		d.State = cardinalityv1alpha1.DropIneffective
		e.pause(cardinalityv1alpha1.ReasonIneffective, fmt.Sprintf(
			"dropping %s left %d of %d series; series also come from outside the selected monitors", d.Metric, series, d.SeriesAtDrop))
		e.event(corev1.EventTypeWarning, cardinalityv1alpha1.ReasonIneffective, "Drop of %s is ineffective: %d of %d series remain; enforcement paused",
			d.Metric, series, d.SeriesAtDrop)
	}
	return nil
}

// cleanUpReverted runs once the budget is no longer paused (after resume): for each Reverted drop it
// removes the rules the operator still owns on other monitors or endpoints, then forgets the drop. While
// paused, Reverted drops are left exactly as they are (ADR 0008). Without this, the remaining ownership
// annotation would be adopted again, re-applying a reverted drop.
func (e *enforcement) cleanUpReverted(ctx context.Context) error {
	// After cleanup the metric is a normal candidate again: resume means "continue", so a budget still over
	// its limit may drop it again one settle period later. To keep a metric, protect it before resuming
	// (ADR 0016).
	if e.pauseReason() != "" || e.r.EnforcementPaused {
		return nil
	}
	for _, d := range slices.Clone(e.budget.Status.EnforcedDrops) {
		if d.State != cardinalityv1alpha1.DropReverted {
			continue
		}
		if err := e.restore(ctx, d.Metric, "cleaned up after resume; a reverted drop is not re-applied"); err != nil {
			return err
		}
	}
	return nil
}

// adoptUnrecorded records owned metrics that are on monitors but missing from enforcedDrops. This
// happens when monitors were written but the status write after it failed; the annotation is the source
// of truth (ADR 0007). Adopted drops re-sync to every selected monitor.
func (e *enforcement) adoptUnrecorded(ctx context.Context) error {
	recorded := map[string]bool{}
	for _, d := range e.budget.Status.EnforcedDrops {
		recorded[d.Metric] = true
	}
	for _, uid := range e.order {
		owned, err := e.monitors[uid].OwnedMetrics()
		if err != nil {
			e.log.Info("ignoring monitor with an unreadable ownership annotation", "monitor", e.monitors[uid].Key(), "error", err.Error())
			continue
		}
		for _, metric := range owned {
			if recorded[metric] {
				continue
			}
			series, err := e.r.Provider.MetricSeries(ctx, e.budget.Namespace, metric)
			if err != nil {
				return errSkipEnforcement
			}
			recorded[metric] = true
			e.resync[metric] = true
			e.budget.Status.EnforcedDrops = append(e.budget.Status.EnforcedDrops, cardinalityv1alpha1.EnforcedDrop{
				Metric: metric, Since: metav1.NewTime(e.now), State: cardinalityv1alpha1.DropPending, SeriesAtDrop: series,
			})
			e.log.Info("adopted a drop found on monitors but not in status", "metric", metric)
		}
	}
	return nil
}

// cleanDeselected removes owned rules from monitors the selector no longer picks: an explicit exclusion,
// so immediately and even while paused (ADR 0009).
func (e *enforcement) cleanDeselected(ctx context.Context) error {
	for _, uid := range e.order {
		m, ok := e.monitors[uid]
		if !ok || e.selected[uid] {
			continue
		}
		owned, err := m.OwnedMetrics()
		if err != nil || len(owned) == 0 {
			continue
		}
		updated := m
		for _, metric := range owned {
			if updated, _, err = updated.WithoutDrop(metric); err != nil {
				return err
			}
		}
		if err := e.write(ctx, uid, updated); err != nil {
			return err
		}
		e.event(corev1.EventTypeNormal, "Deselected", "Removed drop rules for %s from %s: no longer selected",
			strings.Join(owned, ", "), m.Key())
	}
	return nil
}

// restoreProtected restores, immediately, every drop whose metric is now protected (ADR 0009).
func (e *enforcement) restoreProtected(ctx context.Context) error {
	for _, d := range slices.Clone(e.budget.Status.EnforcedDrops) {
		if d.State == cardinalityv1alpha1.DropReverted || !isProtected(d.Metric, e.protected) {
			continue
		}
		if err := e.restore(ctx, d.Metric, "it is now protected"); err != nil {
			return err
		}
	}
	return nil
}

// checkDriftAndExtend compares every active drop with every selected monitor (ADR 0008, ADR 0009). A
// monitor missing the rule is extended when it is new or the budget changed (re-sync); otherwise someone
// removed the rule: the drop becomes Reverted, the budget pauses, and that drop is never written again.
func (e *enforcement) checkDriftAndExtend(ctx context.Context) error {
	paused := e.pauseReason() != "" || e.r.EnforcementPaused
	resyncAll := e.budget.Generation != e.oldGeneration
	for i := range e.budget.Status.EnforcedDrops {
		d := &e.budget.Status.EnforcedDrops[i]
		if d.State == cardinalityv1alpha1.DropReverted {
			continue
		}
		drifted, extended := make([]string, 0, len(e.order)), make([]string, 0, len(e.order))
		for _, uid := range e.order {
			m, ok := e.monitors[uid]
			if !ok || !e.selected[uid] {
				continue
			}
			state, err := m.State(d.Metric)
			if err != nil {
				e.log.Info("ignoring monitor with an unreadable ownership annotation", "monitor", m.Key(), "error", err.Error())
				continue
			}
			if state == enforce.StateOwned || state == enforce.StateTeamRule {
				continue
			}
			isNew := m.Object.GetCreationTimestamp().After(d.Since.Time)
			if !isNew && !resyncAll && !e.resync[d.Metric] {
				drifted = append(drifted, m.Key())
				continue
			}
			if paused {
				continue // no extension while paused (ADR 0008)
			}
			updated, err := m.WithDrop(d.Metric)
			if err != nil {
				return err
			}
			if err := e.write(ctx, uid, updated); err != nil {
				return err
			}
			extended = append(extended, m.Key())
		}
		if len(extended) > 0 {
			e.event(corev1.EventTypeNormal, "Extended", "Extended the drop of %s to %s", d.Metric, strings.Join(extended, ", "))
		}
		if len(drifted) > 0 {
			d.State = cardinalityv1alpha1.DropReverted
			e.pause(cardinalityv1alpha1.ReasonReverted, fmt.Sprintf("the drop rule for %s was removed from %s", d.Metric,
				strings.Join(drifted, ", ")))
			e.event(corev1.EventTypeWarning, cardinalityv1alpha1.ReasonReverted,
				"The drop rule for %s was removed from %s; it will not be re-applied. Enforcement paused until resumed",
				d.Metric, strings.Join(drifted, ", "))
		}
		d.MonitorCount = e.ownedCount(d.Metric)
	}
	return nil
}

// act adds or restores drops according to the mode (HLD step 6 and 7). Restores run while paused;
// new drops do not. The kill switch blocks both.
func (e *enforcement) act(ctx context.Context, settled bool) error {
	spec, st := &e.budget.Spec, &e.budget.Status
	if e.r.EnforcementPaused || !settled {
		return nil
	}
	active := activeDrops(st.EnforcedDrops)
	step := int(spec.Enforcement.MaxMetricsToDrop)
	percent := int32(0)
	if st.UsagePercent != nil {
		percent = *st.UsagePercent
	}

	switch {
	case spec.Mode != cardinalityv1alpha1.ModeEnforce:
		// Leaving Enforce: restore everything gradually, whatever the usage (ADR 0009).
		return e.restoreStep(ctx, step, "the budget is no longer in Enforce mode")
	case len(active) > int(spec.Enforcement.MaxTotalDrops):
		excess := len(active) - int(spec.Enforcement.MaxTotalDrops)
		return e.restoreStep(ctx, min(step, excess), "maxTotalDrops was lowered")
	case percent < spec.WarnThresholdPercent:
		// Below the warn threshold: give metrics back, newest first (ADR 0009).
		return e.restoreStep(ctx, step, "usage is below the warn threshold")
	case percent < 100:
		return nil
	case e.pauseReason() != "" || e.newPause != "":
		return nil
	case st.ExceededSince == nil || e.now.Sub(st.ExceededSince.Time) < spec.Enforcement.GracePeriod.Duration:
		return nil
	}

	candidates := plannedDrops(e.budget, st.TopMetrics, e.protected)
	if len(candidates) == 0 {
		e.limitReached = true
		return nil
	}
	if e.r.MaxDropsPerHour > 0 {
		recent, err := e.dropsLastHour(ctx)
		if err != nil {
			return err
		}
		room := e.r.MaxDropsPerHour - recent
		if room <= 0 {
			e.rateLimited = true
			return nil
		}
		candidates = candidates[:min(len(candidates), room)]
	}
	for _, metric := range candidates {
		if err := e.drop(ctx, metric); err != nil {
			return err
		}
	}
	return nil
}

// drop adds the drop rule for metric to every selected monitor and records it (ADR 0007).
func (e *enforcement) drop(ctx context.Context, metric string) error {
	applied := make([]string, 0, len(e.order))
	for _, uid := range e.order {
		m, ok := e.monitors[uid]
		if !ok || !e.selected[uid] {
			continue
		}
		state, err := m.State(metric)
		if err != nil || state == enforce.StateOwned || state == enforce.StateTeamRule {
			continue
		}
		updated, err := m.WithDrop(metric)
		if err != nil {
			return err
		}
		if err := e.write(ctx, uid, updated); err != nil {
			return err
		}
		applied = append(applied, m.Key())
	}
	if len(applied) == 0 {
		e.log.Info("no selected monitor can take the drop rule", "metric", metric)
		return nil
	}
	e.budget.Status.EnforcedDrops = append(e.budget.Status.EnforcedDrops, cardinalityv1alpha1.EnforcedDrop{
		Metric: metric, MonitorCount: e.ownedCount(metric), Since: metav1.NewTime(e.now),
		State: cardinalityv1alpha1.DropPending, SeriesAtDrop: seriesOf(metric, e.budget.Status.TopMetrics),
	})
	e.event(corev1.EventTypeWarning, "Dropped", "Dropped %s (%d series) on %s", metric,
		seriesOf(metric, e.budget.Status.TopMetrics), strings.Join(applied, ", "))
	return nil
}

// dropsLastHour counts drops started in the last hour across all budgets (ADR 0010): every other budget
// as stored (read without the cache, inside the enforcement mutex) plus this budget as it is now. The
// count is derived from status, never kept in memory. Extending a drop to a new monitor adds no entry,
// so it does not count.
func (e *enforcement) dropsLastHour(ctx context.Context) (int, error) {
	reader := e.r.APIReader
	if reader == nil {
		reader = e.r.Client
	}
	budgets := &cardinalityv1alpha1.CardinalityBudgetList{}
	if err := reader.List(ctx, budgets); err != nil {
		return 0, fmt.Errorf("listing budgets for the hourly drop limit: %w", err)
	}
	since := e.now.Add(-time.Hour)
	count := func(drops []cardinalityv1alpha1.EnforcedDrop) int {
		n := 0
		for _, d := range drops {
			// metav1.Time has its own Before(*metav1.Time); compare the embedded time.Time instead.
			if !d.Since.Time.Before(since) {
				n++
			}
		}
		return n
	}
	total := count(e.budget.Status.EnforcedDrops)
	for i := range budgets.Items {
		if b := &budgets.Items[i]; b.UID != e.budget.UID {
			total += count(b.Status.EnforcedDrops)
		}
	}
	metrics.SetDropsLastHour(total)
	return total, nil
}

// restoreStep restores up to n active drops, most recent first (LIFO, ADR 0009).
func (e *enforcement) restoreStep(ctx context.Context, n int, why string) error {
	active := activeDrops(e.budget.Status.EnforcedDrops)
	slices.SortStableFunc(active, func(a, b cardinalityv1alpha1.EnforcedDrop) int { return b.Since.Compare(a.Since.Time) })
	for _, d := range active[:min(n, len(active))] {
		if err := e.restore(ctx, d.Metric, why); err != nil {
			return err
		}
	}
	return nil
}

// restore removes the owned rule for metric from every monitor and forgets the drop.
func (e *enforcement) restore(ctx context.Context, metric, why string) error {
	for _, uid := range e.order {
		m, ok := e.monitors[uid]
		if !ok {
			continue
		}
		updated, changed, err := m.WithoutDrop(metric)
		if err != nil || !changed {
			continue
		}
		if err := e.write(ctx, uid, updated); err != nil {
			return err
		}
	}
	e.budget.Status.EnforcedDrops = slices.DeleteFunc(e.budget.Status.EnforcedDrops,
		func(d cardinalityv1alpha1.EnforcedDrop) bool { return d.Metric == metric })
	e.event(corev1.EventTypeNormal, "Restored", "Restored %s: %s", metric, why)
	return nil
}

// finishStatus sets the phase and the Enforced condition after all actions.
func (e *enforcement) finishStatus() {
	st, spec := &e.budget.Status, &e.budget.Spec
	active := activeDrops(st.EnforcedDrops)
	if len(active) > 0 && st.Phase != cardinalityv1alpha1.PhaseUnknown {
		st.Phase = cardinalityv1alpha1.PhaseEnforcing
	}
	// computeStatus left plannedDrops nil when nothing is planned; otherwise refresh it after this step's drops.
	if st.PlannedDrops != nil {
		st.PlannedDrops = plannedDrops(e.budget, st.TopMetrics, e.protected)
	}

	gen := e.budget.Generation
	reason := e.pauseReason()
	if reason == "" {
		reason = e.newPause
	}
	switch {
	case reason != "":
		// A pause stays until the platform resumes it, even while the kill switch is on (ADR 0008).
		setCondition(st, cardinalityv1alpha1.ConditionEnforced, metav1.ConditionFalse, reason, e.pauseMessage(reason), gen)
	case e.r.EnforcementPaused && (spec.Mode == cardinalityv1alpha1.ModeEnforce || len(st.EnforcedDrops) > 0):
		setCondition(st, cardinalityv1alpha1.ConditionEnforced, metav1.ConditionFalse, cardinalityv1alpha1.ReasonPaused,
			"monitor writes are blocked by --enforcement-paused", gen)
	case e.rateLimited:
		setCondition(st, cardinalityv1alpha1.ConditionEnforced, metav1.ConditionFalse, cardinalityv1alpha1.ReasonRateLimited,
			fmt.Sprintf("the global limit of %d new drops per hour is reached; this clears as the hour moves on",
				e.r.MaxDropsPerHour), gen)
	case e.limitReached:
		setCondition(st, cardinalityv1alpha1.ConditionEnforced, metav1.ConditionFalse, cardinalityv1alpha1.ReasonLimitReached,
			fmt.Sprintf("over budget, but no metric can be dropped (maxTotalDrops %d, protected and recording-rule metrics skipped)",
				spec.Enforcement.MaxTotalDrops), gen)
	case len(active) > 0:
		setCondition(st, cardinalityv1alpha1.ConditionEnforced, metav1.ConditionTrue, cardinalityv1alpha1.ReasonApplied,
			fmt.Sprintf("%d metrics dropped", len(active)), gen)
	default:
		meta.RemoveStatusCondition(&st.Conditions, cardinalityv1alpha1.ConditionEnforced)
	}
}

// pauseReason returns the stored pause reason, or "" when the budget is not paused.
func (e *enforcement) pauseReason() string {
	return pauseReasonOf(&e.budget.Status)
}

func pauseReasonOf(st *cardinalityv1alpha1.CardinalityBudgetStatus) string {
	c := meta.FindStatusCondition(st.Conditions, cardinalityv1alpha1.ConditionEnforced)
	if c != nil && slices.Contains(cardinalityv1alpha1.PauseReasons, c.Reason) {
		return c.Reason
	}
	return ""
}

// pause raises a pause reason; the first one wins until the budget is resumed.
func (e *enforcement) pause(reason, message string) {
	if e.pauseReason() == "" && e.newPause == "" {
		e.newPause = reason
		e.pauseMessages[reason] = message
	}
}

func (e *enforcement) pauseMessage(reason string) string {
	if msg, ok := e.pauseMessages[reason]; ok {
		return msg + "; resume with the " + cardinalityv1alpha1.ResumeAnnotation + " annotation"
	}
	if c := meta.FindStatusCondition(e.budget.Status.Conditions, cardinalityv1alpha1.ConditionEnforced); c != nil {
		return c.Message
	}
	return reason
}

func (e *enforcement) event(eventType, reason, format string, args ...any) {
	e.events = append(e.events, budgetEvent{Type: eventType, Reason: reason, Note: fmt.Sprintf(format, args...)})
}

// ownedCount counts selected monitors that carry the owned drop rule for metric.
func (e *enforcement) ownedCount(metric string) int32 {
	var n int32
	for uid, m := range e.monitors {
		if s, err := m.State(metric); err == nil && s == enforce.StateOwned && e.selected[uid] {
			if owned, _ := m.OwnedMetrics(); slices.Contains(owned, metric) {
				n++
			}
		}
	}
	return n
}

// activeDrops are the drops whose rules the operator keeps in place (everything but Reverted).
func activeDrops(drops []cardinalityv1alpha1.EnforcedDrop) []cardinalityv1alpha1.EnforcedDrop {
	return slices.DeleteFunc(slices.Clone(drops), func(d cardinalityv1alpha1.EnforcedDrop) bool {
		return d.State == cardinalityv1alpha1.DropReverted
	})
}

func hasPending(drops []cardinalityv1alpha1.EnforcedDrop) bool {
	return slices.ContainsFunc(drops, func(d cardinalityv1alpha1.EnforcedDrop) bool {
		return d.State == cardinalityv1alpha1.DropPending
	})
}

func seriesOf(metric string, top []cardinalityv1alpha1.MetricUsage) int64 {
	for _, t := range top {
		if t.Name == metric {
			return t.Series
		}
	}
	return 0
}
