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

// Package enforce adds and removes the operator's drop rules on ServiceMonitors and PodMonitors and reads
// who owns them (ADR 0007). The rule functions are pure; only Writer talks to the API server.
package enforce

import (
	"regexp"
	"slices"
	"strings"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

const (
	dropAction      = "drop"
	metricNameLabel = "__name__"
)

// DropRule returns the canonical drop rule for a metric (ADR 0007). regexp.QuoteMeta escapes every regex
// character, so no metric name, including Prometheus 3 UTF-8 names with dots, can widen the match.
// Prometheus anchors relabel regexes, so the rule matches exactly this name.
func DropRule(metric string) monitoringv1.RelabelConfig {
	return monitoringv1.RelabelConfig{
		Action:       dropAction,
		SourceLabels: []monitoringv1.LabelName{metricNameLabel},
		Regex:        regexp.QuoteMeta(metric),
	}
}

// IsDropRule reports whether rc is exactly the canonical drop rule for metric. Rules that merely have the
// same effect are not ours and are never touched.
func IsDropRule(rc monitoringv1.RelabelConfig, metric string) bool {
	return strings.EqualFold(rc.Action, dropAction) &&
		len(rc.SourceLabels) == 1 && rc.SourceLabels[0] == metricNameLabel &&
		rc.Regex == regexp.QuoteMeta(metric) &&
		(rc.Separator == nil || *rc.Separator == ";") &&
		rc.TargetLabel == "" && rc.Replacement == nil && rc.Modulus == 0
}

// HasRule reports whether rules contain the canonical drop rule for metric.
func HasRule(rules []monitoringv1.RelabelConfig, metric string) bool {
	return slices.ContainsFunc(rules, func(rc monitoringv1.RelabelConfig) bool { return IsDropRule(rc, metric) })
}

// AppendRule returns a copy of rules with the drop rule appended at the end, so it matches the final
// metric name after the team's own rules. Existing rules keep their content and order.
func AppendRule(rules []monitoringv1.RelabelConfig, metric string) []monitoringv1.RelabelConfig {
	return append(slices.Clone(rules), DropRule(metric))
}

// RemoveLastRule returns a copy of rules without the last occurrence of the canonical drop rule (the one
// AppendRule added), and whether one was found. A missing rule is not an error: cleanup tolerates rules
// that someone else already removed (ADR 0008). All other rules keep their order.
func RemoveLastRule(rules []monitoringv1.RelabelConfig, metric string) ([]monitoringv1.RelabelConfig, bool) {
	for i := len(rules) - 1; i >= 0; i-- {
		if IsDropRule(rules[i], metric) {
			return slices.Delete(slices.Clone(rules), i, i+1), true
		}
	}
	return rules, false
}
