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
	"fmt"
	"math/rand/v2"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
	"github.com/machani/cardinality-budget-operator/internal/metrics"
	"github.com/machani/cardinality-budget-operator/internal/usage"
)

// conflictRetryDelay is the wait before retrying after an optimistic-locking conflict. Conflicts are
// normal, so they are retried quietly instead of being returned as errors.
const conflictRetryDelay = time.Second

// CardinalityBudgetReconciler measures each namespace's usage and reports it on its CardinalityBudget.
// v0.1 never writes to monitors (Observe and Warn only).
type CardinalityBudgetReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Provider usage.Provider
	Recorder events.EventRecorder
	// MeasureInterval is the time between measurements (--measure-interval).
	MeasureInterval time.Duration
	// MaxMeasurementAge is when the shown numbers become Stale (--max-measurement-age).
	MaxMeasurementAge time.Duration
	// Now returns the current time; nil means time.Now. Tests replace it.
	Now func() time.Time
}

// v0.1 RBAC: read budgets, write their status, record Events. Finalizers and monitors come with v0.2.
// +kubebuilder:rbac:groups=cardinality.telemetry-compass.com,resources=cardinalitybudgets,verbs=get;list;watch
// +kubebuilder:rbac:groups=cardinality.telemetry-compass.com,resources=cardinalitybudgets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// Reconcile measures the budget's namespace, writes the status and requeues (HLD "Reconcile loop").
// Error handling (docs/features/v0.1.md, F-0109): only unexpected API errors are returned as errors;
// an untrustworthy measurement is a normal state written to status and retried on the timer.
func (r *CardinalityBudgetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("namespace", req.Namespace, "budget", req.Name)

	budget := &cardinalityv1alpha1.CardinalityBudget{}
	if err := r.Get(ctx, req.NamespacedName, budget); err != nil {
		if apierrors.IsNotFound(err) {
			metrics.DeleteBudget(req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting the budget: %w", err)
	}
	if !budget.DeletionTimestamp.IsZero() {
		// Deletion cleanup arrives with the finalizer in v0.2; v0.1 owns nothing to clean up.
		return ctrl.Result{}, nil
	}

	measurement, measureErr := r.Provider.Measure(ctx, budget.Namespace)
	if measureErr != nil {
		log.Info("measurement not valid; status keeps the last valid numbers", "reason", usage.AsInvalid(measureErr).Reason)
	}

	original := budget.DeepCopy()
	budget.Status = computeStatus(statusInput{
		Budget:            budget,
		Measurement:       measurement,
		MeasureErr:        measureErr,
		Now:               r.now(),
		MaxMeasurementAge: r.MaxMeasurementAge,
	})

	if err := r.Status().Patch(ctx, budget, client.MergeFrom(original)); err != nil {
		switch {
		case apierrors.IsNotFound(err):
			return ctrl.Result{}, nil
		case apierrors.IsConflict(err):
			return ctrl.Result{RequeueAfter: conflictRetryDelay}, nil
		default:
			return ctrl.Result{}, fmt.Errorf("patching the status: %w", err)
		}
	}

	if event := stateEvent(&original.Status, &budget.Status); event != nil {
		r.Recorder.Eventf(budget, nil, event.Type, event.Reason, eventActionMeasure, "%s", event.Note)
		log.Info("budget state changed", "phase", budget.Status.Phase, "reason", event.Reason)
	}
	metrics.SetBudget(budget.Namespace, metrics.BudgetState{
		Limit:        budget.Spec.MaxActiveSeries,
		ActiveSeries: budget.Status.ActiveSeries,
		DropsActive:  len(budget.Status.EnforcedDrops),
		Ready:        readyReason(&budget.Status) == cardinalityv1alpha1.ReasonMeasurementValid,
	})

	return ctrl.Result{RequeueAfter: jitter(r.MeasureInterval)}, nil
}

// SetupWithManager sets up the controller with the Manager. Status writes do not trigger a reconcile
// (they change neither generation nor annotations); otherwise every measurement would immediately cause
// the next one. Timed measurements come from RequeueAfter.
func (r *CardinalityBudgetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cardinalityv1alpha1.CardinalityBudget{}, builder.WithPredicates(
			predicate.Or[client.Object](predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}))).
		Named("cardinalitybudget").
		Complete(r)
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
