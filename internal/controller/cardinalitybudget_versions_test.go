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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	cardinalityv1beta1 "github.com/machani/cardinality-budget-operator/api/v1beta1"
)

// v1alpha1 objects go through unstructured: the operator's scheme knows only v1beta1 (ADR 0017), like a
// user's kubectl that still sends v1alpha1 manifests.
var alphaGVK = schema.GroupVersionKind{
	Group: cardinalityv1beta1.GroupVersion.Group, Version: "v1alpha1", Kind: "CardinalityBudget",
}

func alphaBudget(namespace string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(alphaGVK)
	u.SetNamespace(namespace)
	u.SetName(budgetName)
	return u
}

var _ = Describe("CardinalityBudget API versions", func() {
	ctx := context.Background()

	It("reads a v1alpha1 budget as v1beta1 with every field kept", func() {
		namespace := newTestNamespace(ctx)
		old := alphaBudget(namespace)
		Expect(unstructured.SetNestedMap(old.Object, map[string]any{
			"maxActiveSeries":      int64(1000),
			"warnThresholdPercent": int64(80),
			"mode":                 "Enforce",
			"enforcement": map[string]any{
				"gracePeriod":      "10m",
				"maxMetricsToDrop": int64(2),
				"maxTotalDrops":    int64(4),
				"protectedMetrics": []any{"up", "slo_.*"},
				"monitorSelector":  map[string]any{"matchLabels": map[string]any{"team": "a"}},
			},
		}, "spec")).To(Succeed())
		Expect(k8sClient.Create(ctx, old)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, old)).To(Succeed()) })

		got := &cardinalityv1beta1.CardinalityBudget{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: budgetName, Namespace: namespace}, got)).To(Succeed())
		Expect(got.Spec.MaxActiveSeries).To(Equal(int64(1000)))
		Expect(got.Spec.WarnThresholdPercent).To(Equal(int32(80)))
		Expect(got.Spec.Mode).To(Equal(cardinalityv1beta1.ModeEnforce))
		Expect(got.Spec.Enforcement.GracePeriod.Duration.Minutes()).To(Equal(10.0))
		Expect(got.Spec.Enforcement.MaxMetricsToDrop).To(Equal(int32(2)))
		Expect(got.Spec.Enforcement.MaxTotalDrops).To(Equal(int32(4)))
		Expect(got.Spec.Enforcement.ProtectedMetrics).To(Equal([]string{"up", "slo_.*"}))
		Expect(got.Spec.Enforcement.MonitorSelector.MatchLabels).To(Equal(map[string]string{"team": "a"}))
	})

	It("shows the status written as v1beta1 to v1alpha1 readers", func() {
		namespace := newTestNamespace(ctx)
		budget := budgetIn(namespace, cardinalityv1beta1.CardinalityBudgetSpec{MaxActiveSeries: 100})
		Expect(k8sClient.Create(ctx, budget)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, budget)).To(Succeed()) })

		budget.Status.Phase = cardinalityv1beta1.PhaseExceeded
		budget.Status.ActiveSeries = ptr.To[int64](120)
		budget.Status.UsagePercent = ptr.To[int32](120)
		budget.Status.TopMetrics = []cardinalityv1beta1.MetricUsage{{Name: "http.requests", Series: 90}}
		Expect(k8sClient.Status().Update(ctx, budget)).To(Succeed())

		old := alphaBudget(namespace)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: budgetName, Namespace: namespace}, old)).To(Succeed())
		Expect(old.GetAPIVersion()).To(Equal("cardinality.telemetry-compass.com/v1alpha1"))
		Expect(old.Object["status"]).To(Equal(map[string]any{
			"phase":        "Exceeded",
			"activeSeries": int64(120),
			"usagePercent": int64(120),
			"topMetrics":   []any{map[string]any{"name": "http.requests", "series": int64(90)}},
		}))
	})

	It("applies the same validation to v1alpha1", func() {
		old := alphaBudget(newTestNamespace(ctx))
		old.SetName("team-a")
		Expect(unstructured.SetNestedField(old.Object, int64(10), "spec", "maxActiveSeries")).To(Succeed())
		err := k8sClient.Create(ctx, old)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must be named 'default'"))
	})
})
