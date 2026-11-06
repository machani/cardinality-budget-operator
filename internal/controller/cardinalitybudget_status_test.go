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

	cardinalityv1beta1 "github.com/machani/cardinality-budget-operator/api/v1beta1"
)

// These tests check the status subresource: status is written through its own endpoint, and a normal
// update cannot change it.
var _ = Describe("CardinalityBudget status subresource", func() {
	ctx := context.Background()

	newBudget := func() *cardinalityv1beta1.CardinalityBudget {
		budget := budgetIn(newTestNamespace(ctx), cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 50000})
		Expect(k8sClient.Create(ctx, budget)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, budget)).To(Succeed()) })
		return budget
	}

	get := func(budget *cardinalityv1beta1.CardinalityBudget) *cardinalityv1beta1.CardinalityBudget {
		got := &cardinalityv1beta1.CardinalityBudget{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: budget.Name, Namespace: budget.Namespace}, got)).To(Succeed())
		return got
	}

	It("stores every status field written through the status endpoint", func() {
		budget := newBudget()
		now := metav1.Now()
		budget.Status = cardinalityv1beta1.CardinalityBudgetStatus{
			ObservedGeneration: budget.Generation,
			Phase:              cardinalityv1beta1.PhaseEnforcing,
			ActiveSeries:       ptr.To[int64](61230),
			UsagePercent:       ptr.To[int32](122),
			LastMeasured:       &now,
			TopMetrics:         []cardinalityv1beta1.MetricUsage{{Name: "http_request_duration_seconds_bucket", Series: 18400}},
			PlannedDrops:       []string{"http_requests_total"},
			EnforcedDrops: []cardinalityv1beta1.EnforcedDrop{{
				Metric: "http_request_duration_seconds_bucket", MonitorCount: 4, Since: now,
				State: cardinalityv1beta1.DropEffective,
			}},
		}
		Expect(k8sClient.Status().Update(ctx, budget)).To(Succeed())

		got := get(budget)
		Expect(got.Status.Phase).To(Equal(cardinalityv1beta1.PhaseEnforcing))
		Expect(*got.Status.ActiveSeries).To(Equal(int64(61230)))
		Expect(*got.Status.UsagePercent).To(Equal(int32(122)))
		Expect(got.Status.TopMetrics).To(HaveLen(1))
		Expect(got.Status.EnforcedDrops[0].State).To(Equal(cardinalityv1beta1.DropEffective))
	})

	It("ignores status changes sent with a normal update", func() {
		budget := newBudget()
		budget.Status.Phase = cardinalityv1beta1.PhaseExceeded
		Expect(k8sClient.Update(ctx, budget)).To(Succeed())

		Expect(get(budget).Status.Phase).To(BeEmpty())
	})

	It("rejects an unknown phase", func() {
		budget := newBudget()
		budget.Status.Phase = "Broken"
		Expect(k8sClient.Status().Update(ctx, budget)).NotTo(Succeed())
	})
})
