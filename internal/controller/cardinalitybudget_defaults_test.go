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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	cardinalityv1beta1 "github.com/machani/cardinality-budget-operator/api/v1beta1"
)

// The API server (inside envtest) applies the +kubebuilder:default markers, so these tests check the
// generated CRD schema, not Go code.
var _ = Describe("CardinalityBudget API defaults", func() {
	ctx := context.Background()

	It("fills every default when only maxActiveSeries is set", func() {
		budget := budgetIn(newTestNamespace(ctx), cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 50000})
		Expect(k8sClient.Create(ctx, budget)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, budget)).To(Succeed()) })

		got := &cardinalityv1beta1.CardinalityBudget{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: budgetName, Namespace: budget.Namespace}, got)).To(Succeed())

		Expect(got.Spec.Mode).To(Equal(cardinalityv1beta1.ModeObserve))
		Expect(got.Spec.WarnThresholdPercent).To(Equal(int32(90)))
		Expect(got.Spec.Enforcement.GracePeriod.Duration).To(Equal(30 * time.Minute))
		Expect(got.Spec.Enforcement.MaxMetricsToDrop).To(Equal(int32(1)))
		Expect(got.Spec.Enforcement.MaxTotalDrops).To(Equal(int32(5)))
		Expect(got.Spec.Enforcement.ProtectedMetrics).To(BeEmpty())
		Expect(got.Spec.Enforcement.MonitorSelector).To(BeNil())
	})

	It("rejects values outside the allowed ranges", func() {
		// Rejected objects are never stored, so all cases can share one namespace. Each case checks that
		// the error names the field, so it is rejected for the right reason and not for its name.
		namespace := newTestNamespace(ctx)
		for field, spec := range map[string]cardinalityv1beta1.CardinalityBudgetSpec{
			"maxActiveSeries":      {MaxActiveSeries: 0},
			"warnThresholdPercent": {MaxActiveSeries: 1, WarnThresholdPercent: 100},
			"mode":                 {MaxActiveSeries: 1, Mode: "Delete"},
			"maxTotalDrops": {MaxActiveSeries: 1, Enforcement: cardinalityv1beta1.EnforcementSpec{
				MaxMetricsToDrop: 1, MaxTotalDrops: 11,
			}},
		} {
			err := k8sClient.Create(ctx, budgetIn(namespace, spec))
			Expect(err).To(HaveOccurred(), "case %s should be rejected", field)
			Expect(err.Error()).To(ContainSubstring(field))
		}
	})
})
