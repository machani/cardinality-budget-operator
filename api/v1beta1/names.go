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

package v1beta1

// Names that are part of the API: other tools and people rely on them.
const (
	// BudgetName is the only allowed name of a CardinalityBudget (ADR 0004).
	BudgetName = "default"

	// CleanupFinalizer makes deletion wait until every owned drop rule is removed (ADR 0009).
	CleanupFinalizer = "cardinality.telemetry-compass.com/cleanup"

	// ResumeAnnotation is set on a budget by the platform team to resume a paused budget. Any new value
	// resumes once; the value acted on is stored in status.lastResumeToken (ADR 0008).
	ResumeAnnotation = "cardinality.telemetry-compass.com/resume-enforcement"

	// DroppedMetricsAnnotation on a monitor is the JSON list of metrics whose drop rules this operator owns
	// there. It is the source of truth for ownership (ADR 0007).
	DroppedMetricsAnnotation = "cardinality.telemetry-compass.com/dropped-metrics"

	// EnforcedLabel marks every monitor carrying an owned drop rule, so they can be listed cluster-wide.
	EnforcedLabel = "cardinality.telemetry-compass.com/enforced"
)
