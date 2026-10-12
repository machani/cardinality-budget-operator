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
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// The suite loads the Prometheus Operator CRDs from test/crds, so tests can create the monitors the
// operator will add drop rules to (v0.2).
var _ = Describe("Prometheus Operator CRDs in envtest", func() {
	ctx := context.Background()

	It("stores a ServiceMonitor with a metricRelabelings rule", func() {
		sm := &monitoringv1.ServiceMonitor{
			ObjectMeta: metav1.ObjectMeta{Name: "crd-check", Namespace: testNamespace},
			Spec: monitoringv1.ServiceMonitorSpec{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
				Endpoints: []monitoringv1.Endpoint{{
					Port: "http",
					MetricRelabelConfigs: []monitoringv1.RelabelConfig{{
						Action:       "drop",
						SourceLabels: []monitoringv1.LabelName{"__name__"},
						Regex:        "demo_metric",
					}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, sm)).To(Succeed()) })

		got := &monitoringv1.ServiceMonitor{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sm.Name, Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Spec.Endpoints[0].MetricRelabelConfigs).To(HaveLen(1))
	})
})
