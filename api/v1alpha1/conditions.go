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

// Condition types (HLD "Conditions", ADR 0004).
const (
	// ConditionReady is True when the last reconcile succeeded, the spec is usable and the measurement is
	// valid and fresh.
	ConditionReady = "Ready"
	// ConditionBackendReachable is True when the backend answered and is ready.
	ConditionBackendReachable = "BackendReachable"
	// ConditionBudgetExceeded is True when usage is at or above 100%.
	ConditionBudgetExceeded = "BudgetExceeded"
	// ConditionEnforced reports enforcement (v0.2).
	ConditionEnforced = "Enforced"
)

// Condition reasons (HLD "Conditions", ADR 0005, ADR 0012, ADR 0015).
const (
	// ReasonMeasurementValid: Ready=True.
	ReasonMeasurementValid = "MeasurementValid"
	// ReasonInvalidSpec: a protectedMetrics pattern does not compile (ADR 0012).
	ReasonInvalidSpec = "InvalidSpec"
	// ReasonBackendUnreachable: the backend did not answer or a query failed.
	ReasonBackendUnreachable = "BackendUnreachable"
	// ReasonBackendWarmingUp: the backend is not ready or started less than --backend-warmup ago.
	ReasonBackendWarmingUp = "BackendWarmingUp"
	// ReasonPartialData: a query returned warnings.
	ReasonPartialData = "PartialData"
	// ReasonStale: no valid measurement for longer than --max-measurement-age.
	ReasonStale = "Stale"
	// ReasonBackendReady: BackendReachable=True.
	ReasonBackendReady = "BackendReady"
	// ReasonOverBudget: BudgetExceeded=True.
	ReasonOverBudget = "OverBudget"
	// ReasonWithinBudget: BudgetExceeded=False.
	ReasonWithinBudget = "WithinBudget"
)
