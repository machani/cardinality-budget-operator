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

package enforce

import (
	"encoding/json"
	"fmt"
	"slices"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
)

// Monitor is a ServiceMonitor or a PodMonitor, seen through what enforcement needs: its endpoints'
// metricRelabelings and the ownership annotation. Object is the real API object, so the client can
// read and patch it.
type Monitor struct {
	Object client.Object
}

// DropState is how a monitor relates to one dropped metric.
type DropState int

const (
	// StateMissing: no rule and not owned. A new monitor to extend to, or drift (ADR 0009).
	StateMissing DropState = iota
	// StateOwned: owned, and every endpoint has the rule.
	StateOwned
	// StateTeamRule: not owned, but an endpoint already has an identical rule written by someone else. The
	// operator leaves such a monitor alone for this metric: adding or later removing a rule could touch the
	// team's own rule (ADR 0007).
	StateTeamRule
	// StatePartial: owned, but at least one endpoint lost the rule (drift).
	StatePartial
)

// Key identifies the monitor in logs and Events, for example "ServiceMonitor/api".
func (m Monitor) Key() string {
	return fmt.Sprintf("%s/%s", m.kind(), m.Object.GetName())
}

func (m Monitor) kind() string {
	if _, ok := m.Object.(*monitoringv1.PodMonitor); ok {
		return monitoringv1.PodMonitorsKind
	}
	return monitoringv1.ServiceMonitorsKind
}

// relabelLists returns a pointer to every endpoint's metricRelabelings, so changes apply in place.
func (m Monitor) relabelLists() []*[]monitoringv1.RelabelConfig {
	switch o := m.Object.(type) {
	case *monitoringv1.ServiceMonitor:
		lists := make([]*[]monitoringv1.RelabelConfig, 0, len(o.Spec.Endpoints))
		for i := range o.Spec.Endpoints {
			lists = append(lists, &o.Spec.Endpoints[i].MetricRelabelConfigs)
		}
		return lists
	case *monitoringv1.PodMonitor:
		lists := make([]*[]monitoringv1.RelabelConfig, 0, len(o.Spec.PodMetricsEndpoints))
		for i := range o.Spec.PodMetricsEndpoints {
			lists = append(lists, &o.Spec.PodMetricsEndpoints[i].MetricRelabelConfigs)
		}
		return lists
	default:
		return nil
	}
}

// OwnedMetrics returns the metrics listed in the ownership annotation. An unreadable annotation is an
// error: the caller must then leave the monitor alone rather than guess.
func (m Monitor) OwnedMetrics() ([]string, error) {
	value := m.Object.GetAnnotations()[cardinalityv1alpha1.DroppedMetricsAnnotation]
	if value == "" {
		return nil, nil
	}
	var owned []string
	if err := json.Unmarshal([]byte(value), &owned); err != nil {
		return nil, fmt.Errorf("%s: annotation %s is not a JSON list: %w", m.Key(),
			cardinalityv1alpha1.DroppedMetricsAnnotation, err)
	}
	return owned, nil
}

// State classifies the monitor for one metric.
func (m Monitor) State(metric string) (DropState, error) {
	owned, err := m.OwnedMetrics()
	if err != nil {
		return StateMissing, err
	}
	annotated := slices.Contains(owned, metric)
	lists := m.relabelLists()
	withRule := 0
	for _, rules := range lists {
		if HasRule(*rules, metric) {
			withRule++
		}
	}
	switch {
	case len(lists) == 0:
		// No endpoints: nothing is scraped, so there is nothing to drop.
		return StateOwned, nil
	case annotated && withRule == len(lists):
		return StateOwned, nil
	case annotated:
		return StatePartial, nil
	case withRule > 0:
		return StateTeamRule, nil
	default:
		return StateMissing, nil
	}
}

// WithDrop returns a copy with the drop rule appended to every endpoint that lacks it and the metric
// owned. Call it only for StateMissing or StatePartial.
func (m Monitor) WithDrop(metric string) (Monitor, error) {
	owned, err := m.OwnedMetrics()
	if err != nil {
		return m, err
	}
	updated := Monitor{Object: m.Object.DeepCopyObject().(client.Object)}
	for _, rules := range updated.relabelLists() {
		if !HasRule(*rules, metric) {
			*rules = AppendRule(*rules, metric)
		}
	}
	setOwned(updated.Object, append(owned, metric))
	return updated, nil
}

// WithoutDrop returns a copy with one occurrence of the drop rule removed from every endpoint and the
// metric no longer owned, and whether anything changed. A metric this operator does not own is left
// alone, so a team's identical rule is never removed (ADR 0007).
func (m Monitor) WithoutDrop(metric string) (Monitor, bool, error) {
	owned, err := m.OwnedMetrics()
	if err != nil || !slices.Contains(owned, metric) {
		return m, false, err
	}
	updated := Monitor{Object: m.Object.DeepCopyObject().(client.Object)}
	for _, rules := range updated.relabelLists() {
		*rules, _ = RemoveLastRule(*rules, metric)
	}
	setOwned(updated.Object, slices.DeleteFunc(slices.Clone(owned), func(s string) bool { return s == metric }))
	return updated, true, nil
}

// SameRules reports whether both copies have the same metricRelabelings on every endpoint. A write that
// changes only the annotation or label leaves Prometheus's configuration as it was.
func SameRules(a, b Monitor) bool {
	la, lb := a.relabelLists(), b.relabelLists()
	if len(la) != len(lb) {
		return false
	}
	for i := range la {
		if !equality.Semantic.DeepEqual(*la[i], *lb[i]) {
			return false
		}
	}
	return true
}

// setOwned writes the ownership annotation and label in the same object, so they change in the same
// write as the rules. Both are removed when nothing is owned any more.
func setOwned(obj client.Object, owned []string) {
	owned = slices.Compact(slices.Sorted(slices.Values(owned)))
	annotations, labels := obj.GetAnnotations(), obj.GetLabels()
	if len(owned) == 0 {
		delete(annotations, cardinalityv1alpha1.DroppedMetricsAnnotation)
		delete(labels, cardinalityv1alpha1.EnforcedLabel)
	} else {
		if annotations == nil {
			annotations = map[string]string{}
		}
		if labels == nil {
			labels = map[string]string{}
		}
		value, _ := json.Marshal(owned) // a []string always marshals
		annotations[cardinalityv1alpha1.DroppedMetricsAnnotation] = string(value)
		labels[cardinalityv1alpha1.EnforcedLabel] = "true"
	}
	obj.SetAnnotations(annotations)
	obj.SetLabels(labels)
}
