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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
)

// These tests check the status subresource: status is written through its own endpoint, and a normal
// update cannot change it.
var _ = Describe("CardinalityBudget status subresource", func() {
	ctx := context.Background()

	newBudget := func(name string) *cardinalityv1alpha1.CardinalityBudget {
		budget := &cardinalityv1alpha1.CardinalityBudget{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       cardinalityv1alpha1.CardinalityBudgetSpec{MaxActiveSeries: 50000},
		}
		Expect(k8sClient.Create(ctx, budget)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, budget)).To(Succeed()) })
		return budget
	}

	It("stores every status field written through the status endpoint", func() {
		budget := newBudget("status-roundtrip")
		now := metav1.Now()
		budget.Status = cardinalityv1alpha1.CardinalityBudgetStatus{
			ObservedGeneration: budget.Generation,
			Phase:              cardinalityv1alpha1.PhaseEnforcing,
			ActiveSeries:       ptr.To[int64](61230),
			UsagePercent:       ptr.To[int32](122),
			LastMeasured:       &now,
			TopMetrics:         []cardinalityv1alpha1.MetricUsage{{Name: "http_request_duration_seconds_bucket", Series: 18400}},
			PlannedDrops:       []string{"http_requests_total"},
			EnforcedDrops: []cardinalityv1alpha1.EnforcedDrop{{
				Metric: "http_request_duration_seconds_bucket", MonitorCount: 4, Since: now,
				State: cardinalityv1alpha1.DropEffective,
			}},
		}
		Expect(k8sClient.Status().Update(ctx, budget)).To(Succeed())

		got := &cardinalityv1alpha1.CardinalityBudget{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: budget.Name, Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(cardinalityv1alpha1.PhaseEnforcing))
		Expect(*got.Status.ActiveSeries).To(Equal(int64(61230)))
		Expect(*got.Status.UsagePercent).To(Equal(int32(122)))
		Expect(got.Status.TopMetrics).To(HaveLen(1))
		Expect(got.Status.EnforcedDrops[0].State).To(Equal(cardinalityv1alpha1.DropEffective))
	})

	It("ignores status changes sent with a normal update", func() {
		budget := newBudget("status-ignored")
		budget.Status.Phase = cardinalityv1alpha1.PhaseExceeded
		Expect(k8sClient.Update(ctx, budget)).To(Succeed())

		got := &cardinalityv1alpha1.CardinalityBudget{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: budget.Name, Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Status.Phase).To(BeEmpty())
	})

	It("rejects an unknown phase", func() {
		budget := newBudget("status-bad-phase")
		budget.Status.Phase = "Broken"
		Expect(k8sClient.Status().Update(ctx, budget)).NotTo(Succeed())
	})
})
