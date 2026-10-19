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
	"reflect"
	"regexp"
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
)

// teamRules are rules a team wrote. The operator must never change or reorder them.
func teamRules() []monitoringv1.RelabelConfig {
	sep := ","
	return []monitoringv1.RelabelConfig{
		{Action: "replace", SourceLabels: []monitoringv1.LabelName{"pod"}, TargetLabel: "instance"},
		{Action: "drop", SourceLabels: []monitoringv1.LabelName{"__name__", "job"}, Separator: &sep, Regex: "x;y"},
		{Action: "keep", SourceLabels: []monitoringv1.LabelName{"__name__"}, Regex: "http_.*"},
	}
}

func TestDropRuleEscapesTheName(t *testing.T) {
	for _, name := range []string{"http_requests_total", "http.server.duration", "a+b(c)|d"} {
		rule := DropRule(name)
		re := regexp.MustCompile("^(?:" + rule.Regex + ")$") // Prometheus anchors relabel regexes
		if !re.MatchString(name) {
			t.Errorf("%q: rule %q does not match the name", name, rule.Regex)
		}
		if re.MatchString(name+"x") || re.MatchString("x"+name) {
			t.Errorf("%q: rule %q matches more than the name", name, rule.Regex)
		}
	}
}

func TestAppendAndRemoveKeepTeamRules(t *testing.T) {
	rules := AppendRule(teamRules(), "big_metric")
	if !reflect.DeepEqual(rules[:3], teamRules()) || !IsDropRule(rules[3], "big_metric") {
		t.Fatalf("append changed team rules or did not append at the end: %+v", rules)
	}
	rules, removed := RemoveLastRule(rules, "big_metric")
	if !removed || !reflect.DeepEqual(rules, teamRules()) {
		t.Errorf("remove did not restore the team rules exactly: %+v", rules)
	}
	if _, removed := RemoveLastRule(teamRules(), "big_metric"); removed {
		t.Error("removed a rule that was never added")
	}
}

func TestSimilarRulesAreNotOurs(t *testing.T) {
	keep := DropRule("m")
	keep.Action = "keep"
	withTarget := DropRule("m")
	withTarget.TargetLabel = "x"
	twoLabels := DropRule("m")
	twoLabels.SourceLabels = append(twoLabels.SourceLabels, "job")
	for _, rc := range []monitoringv1.RelabelConfig{keep, withTarget, twoLabels, DropRule("m2")} {
		if IsDropRule(rc, "m") {
			t.Errorf("%+v was taken for the canonical rule", rc)
		}
	}
}

// FuzzAppendRemove checks, for any metric name, that adding then removing our rule returns exactly the
// team's rules (ADR 0007: never remove or reorder the team's rules).
func FuzzAppendRemove(f *testing.F) {
	for _, seed := range []string{"up", "http.server.duration", "a:b", ".*", "x|y", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, metric string) {
		rules, removed := RemoveLastRule(AppendRule(teamRules(), metric), metric)
		if !removed || !reflect.DeepEqual(rules, teamRules()) {
			t.Fatalf("metric %q: team rules changed: %+v", metric, rules)
		}
	})
}

func serviceMonitor(endpoints int, annotations map[string]string) Monitor {
	sm := &monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Name: "api", Annotations: annotations}}
	for range endpoints {
		sm.Spec.Endpoints = append(sm.Spec.Endpoints, monitoringv1.Endpoint{MetricRelabelConfigs: teamRules()})
	}
	return Monitor{Object: sm}
}

func state(t *testing.T, m Monitor, metric string) DropState {
	t.Helper()
	s, err := m.State(metric)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWithDropAndWithoutDrop(t *testing.T) {
	m := serviceMonitor(2, nil)
	if state(t, m, "big") != StateMissing {
		t.Fatal("fresh monitor should be Missing")
	}

	dropped, err := m.WithDrop("big")
	if err != nil {
		t.Fatal(err)
	}
	if state(t, dropped, "big") != StateOwned {
		t.Error("after WithDrop the monitor should be Owned")
	}
	if state(t, dropped, "other") != StateMissing {
		t.Error("dropping one metric must not affect another")
	}
	if dropped.Object.GetLabels()[cardinalityv1alpha1.EnforcedLabel] != "true" {
		t.Error("enforced label missing")
	}
	if state(t, m, "big") != StateMissing {
		t.Error("WithDrop changed the original monitor")
	}

	restored, changed, err := dropped.WithoutDrop("big")
	if err != nil || !changed {
		t.Fatalf("WithoutDrop: changed=%v err=%v", changed, err)
	}
	if !reflect.DeepEqual(restored.Object.(*monitoringv1.ServiceMonitor).Spec, m.Object.(*monitoringv1.ServiceMonitor).Spec) {
		t.Error("restore did not give back the original endpoints")
	}
	if _, ok := restored.Object.GetAnnotations()[cardinalityv1alpha1.DroppedMetricsAnnotation]; ok {
		t.Error("annotation should be removed when nothing is owned")
	}
	if _, ok := restored.Object.GetLabels()[cardinalityv1alpha1.EnforcedLabel]; ok {
		t.Error("label should be removed when nothing is owned")
	}
}

func TestSameRulesIgnoresAnnotationOnlyChanges(t *testing.T) {
	m := serviceMonitor(2, nil)
	dropped, err := m.WithDrop("big")
	if err != nil {
		t.Fatal(err)
	}
	if SameRules(m, dropped) {
		t.Error("adding a drop rule changes the rules")
	}

	// The rules were removed by hand; only our annotation and label are left to clean up.
	handEdited := Monitor{Object: dropped.Object.DeepCopyObject().(*monitoringv1.ServiceMonitor)}
	for i := range handEdited.Object.(*monitoringv1.ServiceMonitor).Spec.Endpoints {
		ep := &handEdited.Object.(*monitoringv1.ServiceMonitor).Spec.Endpoints[i]
		ep.MetricRelabelConfigs, _ = RemoveLastRule(ep.MetricRelabelConfigs, "big")
	}
	cleaned, changed, err := handEdited.WithoutDrop("big")
	if err != nil || !changed {
		t.Fatalf("WithoutDrop: changed=%v err=%v", changed, err)
	}
	if !SameRules(handEdited, cleaned) {
		t.Error("removing only the annotation must not count as a rule change")
	}
}

func TestTeamRuleIsNeverOwnedOrRemoved(t *testing.T) {
	m := serviceMonitor(1, nil)
	sm := m.Object.(*monitoringv1.ServiceMonitor)
	sm.Spec.Endpoints[0].MetricRelabelConfigs = AppendRule(sm.Spec.Endpoints[0].MetricRelabelConfigs, "big")

	if state(t, m, "big") != StateTeamRule {
		t.Fatal("identical rule written by the team should be StateTeamRule")
	}
	if _, changed, _ := m.WithoutDrop("big"); changed {
		t.Error("WithoutDrop removed a rule the operator does not own")
	}
}

func TestPartialDrift(t *testing.T) {
	dropped, _ := serviceMonitor(2, nil).WithDrop("big")
	sm := dropped.Object.(*monitoringv1.ServiceMonitor)
	sm.Spec.Endpoints[1].MetricRelabelConfigs, _ = RemoveLastRule(sm.Spec.Endpoints[1].MetricRelabelConfigs, "big")
	if state(t, dropped, "big") != StatePartial {
		t.Error("owned with a rule missing on one endpoint should be StatePartial")
	}
}

func TestBadAnnotationIsAnError(t *testing.T) {
	m := serviceMonitor(1, map[string]string{cardinalityv1alpha1.DroppedMetricsAnnotation: "not json"})
	if _, err := m.State("big"); err == nil {
		t.Error("an unreadable annotation must be an error, not a guess")
	}
}
