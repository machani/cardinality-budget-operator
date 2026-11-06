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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// After changing this file, run `make manifests generate` (never edit the generated files).
// v1alpha1 is deprecated and removed in v2.0 (ADR 0017). Keep it identical to v1beta1: change both or neither.
// Shared constants (conditions, names) live in v1beta1.
// Defaults and ranges: HLD "API design", ADR 0006 (maxTotalDrops), ADR 0014 (other defaults).

// BudgetMode is how far the operator may act on a budget.
// +kubebuilder:validation:Enum=Observe;Warn;Enforce
type BudgetMode string

const (
	// ModeObserve measures and reports in status only.
	ModeObserve BudgetMode = "Observe"
	// ModeWarn also emits Events and lists plannedDrops.
	ModeWarn BudgetMode = "Warn"
	// ModeEnforce also adds drop rules to monitors when the budget is exceeded.
	ModeEnforce BudgetMode = "Enforce"
)

// CardinalityBudgetSpec defines the desired state of CardinalityBudget.
type CardinalityBudgetSpec struct {
	// maxActiveSeries is the budget: the number of active series the namespace may have.
	// +kubebuilder:validation:Minimum=1
	// +required
	MaxActiveSeries int64 `json:"maxActiveSeries"`

	// warnThresholdPercent is the usage, in percent of maxActiveSeries, at which the budget enters Warning.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=99
	// +kubebuilder:default=90
	// +optional
	WarnThresholdPercent int32 `json:"warnThresholdPercent,omitempty"`

	// mode chooses how far the operator may act. Budgets start in Observe (ADR 0002).
	// +kubebuilder:default=Observe
	// +optional
	Mode BudgetMode `json:"mode,omitempty"`

	// enforcement holds the limits that apply in Enforce mode. Defaulted to {} so that its own
	// defaults apply even when the block is left out.
	// +kubebuilder:default={}
	// +optional
	Enforcement EnforcementSpec `json:"enforcement,omitzero"`
}

// EnforcementSpec limits what the operator may drop in Enforce mode.
// +kubebuilder:validation:XValidation:rule="!has(self.maxMetricsToDrop) || !has(self.maxTotalDrops) || self.maxMetricsToDrop <= self.maxTotalDrops",message="maxMetricsToDrop must be less than or equal to maxTotalDrops"
// +kubebuilder:validation:XValidation:rule="!has(self.gracePeriod) || duration(self.gracePeriod) >= duration('0s')",message="gracePeriod must not be negative"
type EnforcementSpec struct {
	// gracePeriod is how long usage must stay at or above 100% before the first drop.
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:default="30m"
	// +optional
	GracePeriod metav1.Duration `json:"gracePeriod,omitzero"`

	// maxMetricsToDrop is the most metrics dropped in one enforcement step.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=1
	// +optional
	MaxMetricsToDrop int32 `json:"maxMetricsToDrop,omitempty"`

	// maxTotalDrops is the most metrics dropped at the same time for this budget (ADR 0006).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=5
	// +optional
	MaxTotalDrops int32 `json:"maxTotalDrops,omitempty"`

	// protectedMetrics are regular expressions for metric names that are never dropped.
	// Each pattern is fully anchored, like Prometheus (ADR 0012). The rule below compiles every pattern;
	// an invalid one makes the rule fail, so the API server rejects it. The operator checks again at runtime.
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.all(p, ''.matches(p) == ''.matches(p))",message="every protectedMetrics entry must be a valid regular expression (RE2)"
	// +optional
	ProtectedMetrics []string `json:"protectedMetrics,omitempty"`

	// monitorSelector selects the ServiceMonitors and PodMonitors in the namespace that may receive drop
	// rules. Empty or unset selects all of them.
	// +optional
	MonitorSelector *metav1.LabelSelector `json:"monitorSelector,omitempty"`
}

// BudgetPhase summarises the budget's state for humans and printer columns.
// +kubebuilder:validation:Enum=Healthy;Warning;Exceeded;Enforcing;Unknown
type BudgetPhase string

const (
	// PhaseHealthy means usage is below the warn threshold.
	PhaseHealthy BudgetPhase = "Healthy"
	// PhaseWarning means usage is at or above the warn threshold and below 100%.
	PhaseWarning BudgetPhase = "Warning"
	// PhaseExceeded means usage is at or above 100% and nothing is dropped.
	PhaseExceeded BudgetPhase = "Exceeded"
	// PhaseEnforcing means drop rules owned by this budget are in place.
	PhaseEnforcing BudgetPhase = "Enforcing"
	// PhaseUnknown means the last measurement was not valid or is stale (ADR 0005).
	PhaseUnknown BudgetPhase = "Unknown"
)

// DropState is the state of one enforced drop (ADR 0006, ADR 0008).
// +kubebuilder:validation:Enum=Pending;Effective;Ineffective;Reverted
type DropState string

const (
	// DropPending means the rule is applied and the settle period has not ended.
	DropPending DropState = "Pending"
	// DropEffective means the metric's series fell by at least 90% after the settle period.
	DropEffective DropState = "Effective"
	// DropIneffective means the metric's series fell by less than 90%; the budget is paused.
	DropIneffective DropState = "Ineffective"
	// DropReverted means someone removed the rule; it is never re-applied (ADR 0008).
	DropReverted DropState = "Reverted"
)

// MetricUsage is the number of active series of one metric in the namespace.
type MetricUsage struct {
	// name is the metric name.
	// +required
	Name string `json:"name"`

	// series is the number of active series with this name.
	// +required
	Series int64 `json:"series"`
}

// EnforcedDrop records one metric this budget dropped (ADR 0007). Per-monitor detail is in the
// monitor annotations and in Events.
type EnforcedDrop struct {
	// metric is the dropped metric name.
	// +required
	Metric string `json:"metric"`

	// monitorCount is the number of monitors that carry the drop rule.
	// +required
	MonitorCount int32 `json:"monitorCount"`

	// since is when the drop rule was first applied.
	// +required
	Since metav1.Time `json:"since"`

	// state tells whether the drop worked.
	// +required
	State DropState `json:"state"`

	// seriesAtDrop is the metric's number of series in the namespace when it was dropped. The
	// effectiveness check compares against it: Effective when at most 10% remain (ADR 0006, ADR 0016).
	// +optional
	SeriesAtDrop int64 `json:"seriesAtDrop,omitempty"`
}

// CardinalityBudgetStatus defines the observed state of CardinalityBudget.
// Pointers are used where "not measured yet" must differ from zero.
type CardinalityBudgetStatus struct {
	// observedGeneration is the spec generation the status was computed from.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// phase summarises the state of the budget.
	// +optional
	Phase BudgetPhase `json:"phase,omitempty"`

	// activeSeries is the namespace's number of active series at lastMeasured.
	// +optional
	ActiveSeries *int64 `json:"activeSeries,omitempty"`

	// usagePercent is activeSeries as a percentage of maxActiveSeries, rounded down. Can exceed 100.
	// +optional
	UsagePercent *int32 `json:"usagePercent,omitempty"`

	// lastMeasured is the time of the last valid measurement.
	// +optional
	LastMeasured *metav1.Time `json:"lastMeasured,omitempty"`

	// exceededSince is when usage first reached 100%; cleared when usage falls below.
	// +optional
	ExceededSince *metav1.Time `json:"exceededSince,omitempty"`

	// lastEnforcementTime is the start of the settle period after any apply or restore (ADR 0006).
	// +optional
	LastEnforcementTime *metav1.Time `json:"lastEnforcementTime,omitempty"`

	// lastResumeToken is the last resume annotation value acted on (ADR 0008).
	// +optional
	LastResumeToken string `json:"lastResumeToken,omitempty"`

	// topMetrics are the metrics with the most series in the namespace, largest first.
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=10
	// +optional
	TopMetrics []MetricUsage `json:"topMetrics,omitempty"`

	// plannedDrops are the metrics the next enforcement step would drop (Warn and Enforce).
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=10
	// +optional
	PlannedDrops []string `json:"plannedDrops,omitempty"`

	// enforcedDrops are the metrics this budget has dropped, one entry per metric.
	// +listType=map
	// +listMapKey=metric
	// +kubebuilder:validation:MaxItems=10
	// +optional
	EnforcedDrops []EnforcedDrop `json:"enforcedDrops,omitempty"`

	// conditions are Ready, BackendReachable, BudgetExceeded and Enforced (HLD "Conditions").
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:deprecatedversion:warning="cardinality.telemetry-compass.com/v1alpha1 CardinalityBudget is deprecated and will be removed in v2.0; use v1beta1"
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cb
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="a CardinalityBudget must be named 'default' (one per namespace, ADR 0004)"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Series",type=integer,JSONPath=`.status.activeSeries`
// +kubebuilder:printcolumn:name="Limit",type=integer,JSONPath=`.spec.maxActiveSeries`
// +kubebuilder:printcolumn:name="Usage %",type=integer,JSONPath=`.status.usagePercent`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CardinalityBudget is the Schema for the cardinalitybudgets API
type CardinalityBudget struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CardinalityBudget
	// +required
	Spec CardinalityBudgetSpec `json:"spec"`

	// status defines the observed state of CardinalityBudget
	// +optional
	Status CardinalityBudgetStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CardinalityBudgetList contains a list of CardinalityBudget
type CardinalityBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CardinalityBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &CardinalityBudget{}, &CardinalityBudgetList{})
		return nil
	})
}
