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

// Enforced condition reasons (ADR 0006, ADR 0008, ADR 0010). Ineffective, ConfigReloadFailed and Reverted
// pause the budget until the platform resumes it; RateLimited and LimitReached clear on their own; Paused
// reflects the global kill switch.
const (
	// ReasonApplied: Enforced=True, drop rules are in place and enforcement may continue.
	ReasonApplied = "Applied"
	// ReasonIneffective: a drop removed less than 90% of its metric's series.
	ReasonIneffective = "Ineffective"
	// ReasonLimitReached: over budget, but maxTotalDrops is reached or no candidate is left.
	ReasonLimitReached = "LimitReached"
	// ReasonConfigReloadFailed: Prometheus did not reload its config within 5 minutes of a change.
	ReasonConfigReloadFailed = "ConfigReloadFailed"
	// ReasonReverted: someone removed an owned drop rule; it is never re-applied.
	ReasonReverted = "Reverted"
	// ReasonRateLimited: the global hourly drop limit is reached (v0.3).
	ReasonRateLimited = "RateLimited"
	// ReasonPaused: the global kill switch --enforcement-paused is on.
	ReasonPaused = "Paused"
)

// PauseReasons are the Enforced reasons that pause a budget until it is resumed (ADR 0008).
var PauseReasons = []string{ReasonIneffective, ReasonConfigReloadFailed, ReasonReverted}
