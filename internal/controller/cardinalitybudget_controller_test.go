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
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
	"github.com/machani/cardinality-budget-operator/internal/testutil"
	"github.com/machani/cardinality-budget-operator/internal/usage"
)

// Integration tests (F-0114): the real reconciler against the envtest API server and a fake Prometheus.
// Reconcile is called directly, one measurement per call, so each step is deterministic.
var _ = Describe("CardinalityBudget reconcile", func() {
	ctx := context.Background()

	var (
		fake       *testutil.FakePrometheus
		provider   *usage.PrometheusProvider
		recorder   *events.FakeRecorder
		reconciler *CardinalityBudgetReconciler
	)

	BeforeEach(func() {
		fake = testutil.NewFakePrometheus()
		DeferCleanup(fake.Close)
		var err error
		provider, err = usage.NewPrometheusProvider(usage.Settings{
			URL: fake.URL(), NamespaceLabel: "namespace", QueryTimeout: 5 * time.Second, BackendWarmup: 15 * time.Minute,
		})
		Expect(err).NotTo(HaveOccurred())
		recorder = events.NewFakeRecorder(50)
		reconciler = &CardinalityBudgetReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(), Provider: provider, Recorder: recorder,
			MeasureInterval: 5 * time.Minute, MaxMeasurementAge: 10 * time.Minute,
		}
	})

	create := func(spec cardinalityv1alpha1.CardinalityBudgetSpec) *cardinalityv1alpha1.CardinalityBudget {
		budget := budgetIn(newTestNamespace(ctx), spec)
		Expect(k8sClient.Create(ctx, budget)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, budget))).To(Succeed()) })
		return budget
	}

	reconcileOnce := func(budget *cardinalityv1alpha1.CardinalityBudget) (reconcile.Result, *cardinalityv1alpha1.CardinalityBudget) {
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(budget)})
		Expect(err).NotTo(HaveOccurred())
		got := &cardinalityv1alpha1.CardinalityBudget{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(budget), got)).To(Succeed())
		return result, got
	}

	setUsage := func(namespace string, series float64, top ...testutil.Sample) {
		fake.SetResult(provider.CountQuery(namespace), testutil.Sample{Value: series})
		fake.SetResult(provider.TopQuery(namespace), top...)
	}

	drainEvents := func() []string {
		var got []string
		for {
			select {
			case e := <-recorder.Events:
				got = append(got, e)
			default:
				return got
			}
		}
	}

	It("moves through Healthy, Warning, Exceeded and back, with one Event per change", func() {
		budget := create(cardinalityv1alpha1.CardinalityBudgetSpec{MaxActiveSeries: 1000})
		for _, step := range []struct {
			series float64
			phase  cardinalityv1alpha1.BudgetPhase
		}{
			{100, cardinalityv1alpha1.PhaseHealthy},
			{150, cardinalityv1alpha1.PhaseHealthy},
			{950, cardinalityv1alpha1.PhaseWarning},
			{1200, cardinalityv1alpha1.PhaseExceeded},
			{100, cardinalityv1alpha1.PhaseHealthy},
		} {
			setUsage(budget.Namespace, step.series)
			_, got := reconcileOnce(budget)
			Expect(got.Status.Phase).To(Equal(step.phase), "at %v series", step.series)
			Expect(*got.Status.ActiveSeries).To(Equal(int64(step.series)))
			Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
		}

		reasons := []string{}
		for _, e := range drainEvents() {
			reasons = append(reasons, strings.Fields(e)[1])
		}
		Expect(reasons).To(Equal([]string{"BudgetHealthy", "BudgetWarning", "BudgetExceeded", "BudgetHealthy"}))
	})

	It("requeues after about the measure interval", func() {
		budget := create(cardinalityv1alpha1.CardinalityBudgetSpec{MaxActiveSeries: 1000})
		result, _ := reconcileOnce(budget)
		Expect(result.RequeueAfter).To(BeNumerically("~", 5*time.Minute, 30*time.Second))
	})

	It("reports Unknown and keeps the last numbers when Prometheus fails", func() {
		budget := create(cardinalityv1alpha1.CardinalityBudgetSpec{MaxActiveSeries: 1000})
		setUsage(budget.Namespace, 400)
		reconcileOnce(budget)

		fake.SetQueryError(http.StatusServiceUnavailable, "overloaded")
		result, got := reconcileOnce(budget)

		Expect(got.Status.Phase).To(Equal(cardinalityv1alpha1.PhaseUnknown))
		Expect(*got.Status.ActiveSeries).To(Equal(int64(400)))
		ready := meta.FindStatusCondition(got.Status.Conditions, cardinalityv1alpha1.ConditionReady)
		Expect(ready.Reason).To(Equal(cardinalityv1alpha1.ReasonBackendUnreachable))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "an invalid measurement is retried on the timer, not as an error")
	})

	It("reports warming up right after a Prometheus restart", func() {
		budget := create(cardinalityv1alpha1.CardinalityBudgetSpec{MaxActiveSeries: 1000})
		fake.SetRuntimeInfo(time.Now().Add(-time.Minute), true, time.Now())
		_, got := reconcileOnce(budget)
		Expect(got.Status.Phase).To(Equal(cardinalityv1alpha1.PhaseUnknown))
		Expect(meta.FindStatusCondition(got.Status.Conditions, cardinalityv1alpha1.ConditionReady).Reason).
			To(Equal(cardinalityv1alpha1.ReasonBackendWarmingUp))
		Expect(got.Status.ActiveSeries).To(BeNil(), "never measured, so no numbers")
	})

	It("plans drops in Warn mode but not in Observe mode", func() {
		top := []testutil.Sample{
			{Labels: map[string]string{"__name__": "http_requests_total"}, Value: 700},
			{Labels: map[string]string{"__name__": "up"}, Value: 600},
		}
		for _, mode := range []cardinalityv1alpha1.BudgetMode{cardinalityv1alpha1.ModeObserve, cardinalityv1alpha1.ModeWarn} {
			budget := create(cardinalityv1alpha1.CardinalityBudgetSpec{MaxActiveSeries: 1000, Mode: mode,
				Enforcement: cardinalityv1alpha1.EnforcementSpec{ProtectedMetrics: []string{"up"}}})
			setUsage(budget.Namespace, 1200, top...)
			_, got := reconcileOnce(budget)
			if mode == cardinalityv1alpha1.ModeObserve {
				Expect(got.Status.PlannedDrops).To(BeEmpty())
			} else {
				Expect(got.Status.PlannedDrops).To(Equal([]string{"http_requests_total"}))
			}
			Expect(got.Status.TopMetrics).To(HaveLen(2))
		}
	})

	It("ignores a budget that no longer exists", func() {
		result, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: budgetName, Namespace: "does-not-exist"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
	})
})
