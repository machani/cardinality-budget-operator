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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
)

// eventActionMeasure is the events.k8s.io "action" of every measurement Event.
const eventActionMeasure = "Measure"

// budgetEvent is one Event to record on the budget.
type budgetEvent struct {
	Type   string // corev1.EventTypeNormal or corev1.EventTypeWarning
	Reason string
	Note   string
}

// stateEvent returns the Event for a state change, or nil when nothing changed. The state is the phase
// plus the Ready reason, so a budget emits one Event when it goes from Healthy to Warning, and one when
// the backend goes from warming up to unreachable, but none on every unchanged measurement. Every mode
// emits these Events (ADR 0015).
func stateEvent(old, updated *cardinalityv1alpha1.CardinalityBudgetStatus) *budgetEvent {
	oldReason, newReason := readyReason(old), readyReason(updated)
	if old.Phase == updated.Phase && oldReason == newReason {
		return nil
	}

	if ready := meta.FindStatusCondition(updated.Conditions, cardinalityv1alpha1.ConditionReady); ready != nil &&
		ready.Reason != cardinalityv1alpha1.ReasonMeasurementValid {
		return &budgetEvent{Type: corev1.EventTypeWarning, Reason: ready.Reason,
			Note: fmt.Sprintf("Budget is %s: %s", updated.Phase, ready.Message)}
	}

	usage := ""
	if updated.ActiveSeries != nil && updated.UsagePercent != nil {
		usage = fmt.Sprintf(": %d series, %d%% of the budget", *updated.ActiveSeries, *updated.UsagePercent)
	}
	eventType := corev1.EventTypeWarning
	if updated.Phase == cardinalityv1alpha1.PhaseHealthy {
		eventType = corev1.EventTypeNormal
	}
	return &budgetEvent{Type: eventType, Reason: "Budget" + string(updated.Phase),
		Note: fmt.Sprintf("Budget is %s%s", updated.Phase, usage)}
}
