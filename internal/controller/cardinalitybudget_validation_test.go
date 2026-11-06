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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cardinalityv1beta1 "github.com/machani/cardinality-budget-operator/api/v1beta1"
)

// CEL rules (x-kubernetes-validations) run inside the API server. These tests also answer Phase 1
// facts 1 (root rule can read metadata.name) and 9 (CEL can reject an invalid regex).
var _ = Describe("CardinalityBudget CEL validation", func() {
	ctx := context.Background()

	expectRejected := func(budget *cardinalityv1beta1.CardinalityBudget, message string) {
		err := k8sClient.Create(ctx, budget)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(message))
	}

	expectAccepted := func(budget *cardinalityv1beta1.CardinalityBudget) {
		Expect(k8sClient.Create(ctx, budget)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, budget)).To(Succeed()) })
	}

	It("rejects a budget not named default (ADR 0004, fact 1)", func() {
		budget := budgetIn(newTestNamespace(ctx), cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 1000})
		budget.Name = "payments"
		expectRejected(budget, "must be named 'default'")
	})

	It("rejects maxMetricsToDrop above maxTotalDrops (ADR 0006)", func() {
		spec := cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 1000,
			Enforcement: cardinalityv1beta1.EnforcementSpec{MaxMetricsToDrop: 6, MaxTotalDrops: 5}}
		expectRejected(budgetIn(newTestNamespace(ctx), spec), "less than or equal to maxTotalDrops")
	})

	It("accepts maxMetricsToDrop up to the defaulted maxTotalDrops", func() {
		spec := cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 1000,
			Enforcement: cardinalityv1beta1.EnforcementSpec{MaxMetricsToDrop: 5}}
		expectAccepted(budgetIn(newTestNamespace(ctx), spec))
	})

	It("rejects a negative gracePeriod (ADR 0014)", func() {
		spec := cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 1000,
			Enforcement: cardinalityv1beta1.EnforcementSpec{GracePeriod: metav1.Duration{Duration: -5 * time.Minute}}}
		expectRejected(budgetIn(newTestNamespace(ctx), spec), "gracePeriod")
	})

	It("rejects an invalid protectedMetrics regex (ADR 0012, fact 9)", func() {
		spec := cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 1000,
			Enforcement: cardinalityv1beta1.EnforcementSpec{ProtectedMetrics: []string{"up", "slo_(.*"}}}
		expectRejected(budgetIn(newTestNamespace(ctx), spec), "protectedMetrics")
	})

	It("rejects too many or too long protectedMetrics (ADR 0012)", func() {
		namespace := newTestNamespace(ctx)
		tooMany := make([]string, 65)
		for i := range tooMany {
			tooMany[i] = "metric"
		}
		for _, patterns := range [][]string{tooMany, {strings.Repeat("a", 257)}, {""}} {
			spec := cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 1000,
				Enforcement: cardinalityv1beta1.EnforcementSpec{ProtectedMetrics: patterns}}
			expectRejected(budgetIn(namespace, spec), "protectedMetrics")
		}
	})

	It("accepts valid protectedMetrics patterns", func() {
		spec := cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 1000,
			Enforcement: cardinalityv1beta1.EnforcementSpec{ProtectedMetrics: []string{"up", "slo_.*", "^kube_.+$"}}}
		expectAccepted(budgetIn(newTestNamespace(ctx), spec))
	})
})
