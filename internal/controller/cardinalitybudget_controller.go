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
	"math/rand/v2"
	"sync"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cardinalityv1beta1 "github.com/machani/cardinality-budget-operator/api/v1beta1"
	"github.com/machani/cardinality-budget-operator/internal/enforce"
	"github.com/machani/cardinality-budget-operator/internal/metrics"
	"github.com/machani/cardinality-budget-operator/internal/usage"
)

// conflictRetryDelay is the wait before retrying after an optimistic-locking conflict or a stale cache.
// Conflicts are normal, so they are retried quietly instead of being returned as errors.
const conflictRetryDelay = time.Second

// CardinalityBudgetReconciler measures each namespace's usage, reports it on its CardinalityBudget and,
// in Enforce mode, adds and removes drop rules on the namespace's monitors.
type CardinalityBudgetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// APIReader reads budgets without the cache inside the enforcement step (ADR 0010).
	APIReader client.Reader
	Provider  usage.Provider
	Recorder  events.EventRecorder
	// MeasureInterval is the time between measurements (--measure-interval).
	MeasureInterval time.Duration
	// MaxMeasurementAge is when the shown numbers become Stale (--max-measurement-age).
	MaxMeasurementAge time.Duration
	// SettlePeriod is the wait between enforcement steps (--enforcement-settle-period).
	SettlePeriod time.Duration
	// EnforcementPaused is the kill switch (--enforcement-paused): no monitor writes at all.
	EnforcementPaused bool
	// MaxDropsPerHour is the global limit on new drops across all budgets (--max-drops-per-hour,
	// ADR 0010). 0 means no limit; the flag never allows 0, only tests leave it unset.
	MaxDropsPerHour int
	// Now returns the current time; nil means time.Now. Tests replace it.
	Now func() time.Time

	// enforceMu serialises the enforcement step and its status write across budgets (ADR 0010). It
	// coordinates work only; no state lives in memory.
	enforceMu sync.Mutex
}

// +kubebuilder:rbac:groups=cardinality.telemetry-compass.com,resources=cardinalitybudgets,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=cardinality.telemetry-compass.com,resources=cardinalitybudgets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cardinality.telemetry-compass.com,resources=cardinalitybudgets/finalizers,verbs=update
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;podmonitors,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// Reconcile follows HLD "Reconcile loop". Error handling (docs/features/v0.1.md): only unexpected API
// errors are returned as errors; untrustworthy measurements and conflicts are retried on a timer.
func (r *CardinalityBudgetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("namespace", req.Namespace, "budget", req.Name)
	ctx = logf.IntoContext(ctx, log)

	budget := &cardinalityv1beta1.CardinalityBudget{}
	if err := r.Get(ctx, req.NamespacedName, budget); err != nil {
		if apierrors.IsNotFound(err) {
			metrics.DeleteBudget(req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting the budget: %w", err)
	}

	// Step 1: deletion removes every owned rule, then the finalizer.
	if !budget.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, budget)
	}
	if !controllerutil.ContainsFinalizer(budget, cardinalityv1beta1.CleanupFinalizer) {
		original := budget.DeepCopy()
		controllerutil.AddFinalizer(budget, cardinalityv1beta1.CleanupFinalizer)
		if err := r.Patch(ctx, budget, client.MergeFrom(original)); err != nil {
			return r.handleWriteError(err, "adding the finalizer")
		}
	}

	original := budget.DeepCopy()
	oldGeneration := budget.Status.ObservedGeneration
	var budgetEvents []budgetEvent

	// Step 2: resume.
	if e := r.resume(budget); e != nil {
		budgetEvents = append(budgetEvents, *e)
	}

	// Steps 3 and 4: measure, compute phase and conditions.
	measurement, measureErr := r.Provider.Measure(ctx, budget.Namespace)
	if measureErr != nil {
		log.Info("measurement not valid; no drop rules added or removed", "reason", usage.AsInvalid(measureErr).Reason)
	}
	now := r.now()
	budget.Status = computeStatus(statusInput{
		Budget: budget, Measurement: measurement, MeasureErr: measureErr, Now: now, MaxMeasurementAge: r.MaxMeasurementAge,
	})
	result := ctrl.Result{RequeueAfter: jitter(r.MeasureInterval)}

	// Steps 5 to 8 under the enforcement mutex: enforcement and the status write that records it.
	r.enforceMu.Lock()
	defer r.enforceMu.Unlock()

	protected, selector, specErr := compileSpec(budget)
	if measureErr == nil && specErr == nil {
		fresh, err := r.isCacheFresh(ctx, original)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !fresh {
			return ctrl.Result{RequeueAfter: conflictRetryDelay}, nil
		}
		e := newEnforcement(r, log, budget, oldGeneration, now, protected)
		requeue, err := e.run(ctx, selector)
		switch {
		case errors.Is(err, errSkipEnforcement):
			log.Info("enforcement skipped this time", "reason", err.Error())
		case err != nil:
			return r.handleWriteError(err, "enforcing")
		}
		budgetEvents = append(budgetEvents, e.events...)
		if requeue > 0 && requeue < result.RequeueAfter {
			result.RequeueAfter = requeue
		}
	}

	if err := r.Status().Patch(ctx, budget, client.MergeFrom(original)); err != nil {
		return r.handleWriteError(err, "patching the status")
	}

	if e := stateEvent(&original.Status, &budget.Status); e != nil {
		budgetEvents = append(budgetEvents, *e)
	}
	if e := enforcedEvent(&original.Status, &budget.Status); e != nil {
		budgetEvents = append(budgetEvents, *e)
	}
	for _, e := range mergeEvents(budgetEvents) {
		r.Recorder.Eventf(budget, nil, e.Type, e.Reason, eventActionMeasure, "%s", e.Note)
		log.Info("budget event", "reason", e.Reason, "note", e.Note)
	}
	metrics.SetBudget(budget.Namespace, metrics.BudgetState{
		Limit:        budget.Spec.MaxActiveSeries,
		ActiveSeries: budget.Status.ActiveSeries,
		DropsActive:  len(activeDrops(budget.Status.EnforcedDrops)),
		Ready:        readyReason(&budget.Status) == cardinalityv1beta1.ReasonMeasurementValid,
		PauseReason:  pauseReasonOf(&budget.Status),
	})
	return result, nil
}

// resume clears a pause when the resume annotation has a value not acted on yet (ADR 0008): the pause
// is lifted and the token is recorded. Reverted drops are then cleaned up by the enforcement step: their
// leftover rules are removed (never re-applied) and their entries dropped (ADR 0016).
func (r *CardinalityBudgetReconciler) resume(budget *cardinalityv1beta1.CardinalityBudget) *budgetEvent {
	token := budget.Annotations[cardinalityv1beta1.ResumeAnnotation]
	st := &budget.Status
	if token == "" || token == st.LastResumeToken {
		return nil
	}
	st.LastResumeToken = token
	reason := pauseReasonOf(st)
	if reason == "" {
		return nil
	}
	// Ineffective drops keep their rule and their state; only the pause is lifted.
	meta.RemoveStatusCondition(&st.Conditions, cardinalityv1beta1.ConditionEnforced)
	return &budgetEvent{Type: corev1.EventTypeNormal, Reason: "Resumed",
		Note: fmt.Sprintf("Enforcement resumed (was paused: %s)", reason)}
}

// finalize removes every owned drop rule from the namespace's monitors, then the finalizer. While the
// kill switch is on, deletion waits (ADR 0010); the runbook describes manual removal by label.
func (r *CardinalityBudgetReconciler) finalize(ctx context.Context,
	budget *cardinalityv1beta1.CardinalityBudget) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(budget, cardinalityv1beta1.CleanupFinalizer) {
		return ctrl.Result{}, nil
	}
	if r.EnforcementPaused {
		log.Info("deletion waits: monitor writes are blocked by --enforcement-paused")
		return ctrl.Result{RequeueAfter: r.MeasureInterval}, nil
	}

	r.enforceMu.Lock()
	defer r.enforceMu.Unlock()
	monitors, err := enforce.ListOwned(ctx, r.Client, budget.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing enforced monitors: %w", err)
	}
	writer := &enforce.Writer{Client: r.Client, Paused: r.EnforcementPaused}
	for _, m := range monitors {
		owned, err := m.OwnedMetrics()
		if err != nil {
			return ctrl.Result{}, err
		}
		updated := m
		for _, metric := range owned {
			if updated, _, err = updated.WithoutDrop(metric); err != nil {
				return ctrl.Result{}, err
			}
		}
		if err := writer.Patch(ctx, m, updated); err != nil && !apierrors.IsNotFound(err) {
			return r.handleWriteError(err, "removing drop rules from "+m.Key())
		}
	}

	original := budget.DeepCopy()
	controllerutil.RemoveFinalizer(budget, cardinalityv1beta1.CleanupFinalizer)
	if err := r.Patch(ctx, budget, client.MergeFrom(original)); err != nil {
		return r.handleWriteError(err, "removing the finalizer")
	}
	metrics.DeleteBudget(budget.Namespace)
	log.Info("budget deleted; all owned drop rules removed", "monitors", len(monitors))
	return ctrl.Result{}, nil
}

// isCacheFresh re-reads the budget without the cache (ADR 0010). If the cache is behind (for example
// it has not seen our last status write yet), enforcement could act on old enforcedDrops, so it waits.
func (r *CardinalityBudgetReconciler) isCacheFresh(ctx context.Context, cached *cardinalityv1beta1.CardinalityBudget) (bool, error) {
	if r.APIReader == nil {
		return true, nil
	}
	fresh := &cardinalityv1beta1.CardinalityBudget{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(cached), fresh); err != nil {
		return false, fmt.Errorf("reading the budget without cache: %w", err)
	}
	return fresh.ResourceVersion == cached.ResourceVersion, nil
}

// handleWriteError maps write errors to the error-handling table: conflicts retry quietly, a vanished
// object is done, anything else is returned for backoff.
func (r *CardinalityBudgetReconciler) handleWriteError(err error, what string) (ctrl.Result, error) {
	switch {
	case apierrors.IsConflict(err):
		return ctrl.Result{RequeueAfter: conflictRetryDelay}, nil
	case apierrors.IsNotFound(err):
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, fmt.Errorf("%s: %w", what, err)
	}
}

// SetupWithManager sets up the controller. Status writes do not trigger a reconcile (they change neither
// generation nor annotations); deletion does. Monitor changes trigger the namespace's budget, so drift,
// new monitors and selection changes are seen without waiting for the timer.
func (r *CardinalityBudgetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	deleting := predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		return !e.ObjectNew.GetDeletionTimestamp().IsZero()
	}}
	toBudget := handler.EnqueueRequestsFromMapFunc(budgetForMonitor)
	return ctrl.NewControllerManagedBy(mgr).
		For(&cardinalityv1beta1.CardinalityBudget{}, builder.WithPredicates(predicate.Or[client.Object](
			predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}, deleting))).
		Watches(&monitoringv1.ServiceMonitor{}, toBudget).
		Watches(&monitoringv1.PodMonitor{}, toBudget).
		Named("cardinalitybudget").
		Complete(r)
}

// budgetForMonitor maps a ServiceMonitor or PodMonitor event to its namespace's budget: there is at most
// one, always named "default" (ADR 0004). A namespace without a budget costs one cheap NotFound.
func budgetForMonitor(_ context.Context, monitor client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: types.NamespacedName{
		Namespace: monitor.GetNamespace(), Name: cardinalityv1beta1.BudgetName}}}
}

func (r *CardinalityBudgetReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// jitter spreads requeues by up to ±10% so budgets do not all query Prometheus at the same moment.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	spread := d / 5
	return d - spread/2 + time.Duration(rand.Int64N(int64(spread)+1))
}
