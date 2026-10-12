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

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
)

const (
	// testNamespace is the namespace for test objects that are not budgets (for example monitors).
	testNamespace = "default"
	// budgetName is the only name a CardinalityBudget may have (ADR 0004).
	budgetName = "default"
)

// newTestNamespace creates a namespace with a unique name. Budgets must be named "default", so every test
// that creates a budget uses its own namespace. envtest has no namespace controller, so namespaces are
// never removed; the generated names never clash.
func newTestNamespace(ctx context.Context) string {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "cb-test-"}}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	return ns.Name
}

// budgetIn returns an unsaved budget named "default" in the namespace.
func budgetIn(namespace string, spec cardinalityv1alpha1.CardinalityBudgetSpec) *cardinalityv1alpha1.CardinalityBudget {
	return &cardinalityv1alpha1.CardinalityBudget{
		ObjectMeta: metav1.ObjectMeta{Name: budgetName, Namespace: namespace},
		Spec:       spec,
	}
}
