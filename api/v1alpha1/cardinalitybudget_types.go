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
	// Each pattern is fully anchored, like Prometheus (ADR 0012).
	// +listType=atomic
	// +optional
	ProtectedMetrics []string `json:"protectedMetrics,omitempty"`

	// monitorSelector selects the ServiceMonitors and PodMonitors in the namespace that may receive drop
	// rules. Empty or unset selects all of them.
	// +optional
	MonitorSelector *metav1.LabelSelector `json:"monitorSelector,omitempty"`
}

// CardinalityBudgetStatus defines the observed state of CardinalityBudget.
type CardinalityBudgetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the CardinalityBudget resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
