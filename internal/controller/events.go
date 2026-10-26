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
	"slices"
	"strings"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"

	cardinalityv1alpha1 "github.com/machani/cardinality-budget-operator/api/v1alpha1"
)

// eventActionMeasure is the events.k8s.io "action" of every measurement Event.
const eventActionMeasure = "Measure"

// maxEventNote is the events.k8s.io limit on an Event's note, in bytes.
const maxEventNote = 1024

// budgetEvent is one Event to record on the budget.
type budgetEvent struct {
	Type   string // corev1.EventTypeNormal or corev1.EventTypeWarning
	Reason string
	Note   string
	// Tail is said once after all the notes it applies to when Events are combined, for example the
	// reason shared by several restores.
	Tail string
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

// mergeEvents combines the Events of one reconcile that share type and reason into one Event, in
// first-seen order. The Events API folds Events with the same reason sent within 6 minutes into a series
// that keeps only the first note, so five separate Reverted Events would show only the first metric.
// Notes with the same tail are joined, and the tail is said once in parentheses after them.
func mergeEvents(in []budgetEvent) []budgetEvent {
	type tailGroup struct {
		tail  string
		notes []string
	}
	type merged struct {
		event  budgetEvent
		groups []tailGroup
	}
	var out []merged
	index := map[string]int{}
	for _, e := range in {
		key := e.Type + "/" + e.Reason
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, merged{event: budgetEvent{Type: e.Type, Reason: e.Reason}})
		}
		m := &out[i]
		g := slices.IndexFunc(m.groups, func(g tailGroup) bool { return g.tail == e.Tail })
		if g < 0 {
			m.groups = append(m.groups, tailGroup{tail: e.Tail})
			g = len(m.groups) - 1
		}
		m.groups[g].notes = append(m.groups[g].notes, e.Note)
	}
	events := make([]budgetEvent, 0, len(out))
	for _, m := range out {
		parts := make([]string, 0, len(m.groups))
		for _, g := range m.groups {
			part := strings.Join(g.notes, "; ")
			if g.tail != "" {
				part += " (" + g.tail + ")"
			}
			parts = append(parts, part)
		}
		m.event.Note = truncateNote(strings.Join(parts, "; "))
		events = append(events, m.event)
	}
	return events
}

// truncateNote cuts a note to the API limit on a UTF-8 boundary; status keeps the full record.
func truncateNote(note string) string {
	if len(note) <= maxEventNote {
		return note
	}
	const more = " …"
	cut := maxEventNote - len(more)
	for cut > 0 && !utf8.RuneStart(note[cut]) {
		cut--
	}
	return note[:cut] + more
}

// enforcedEvent returns an Event when the Enforced condition changes to Paused (kill switch) or
// LimitReached. Drops, restores and pauses already record their own Event where they happen.
func enforcedEvent(old, updated *cardinalityv1alpha1.CardinalityBudgetStatus) *budgetEvent {
	before := meta.FindStatusCondition(old.Conditions, cardinalityv1alpha1.ConditionEnforced)
	after := meta.FindStatusCondition(updated.Conditions, cardinalityv1alpha1.ConditionEnforced)
	if after == nil || (before != nil && before.Reason == after.Reason) {
		return nil
	}
	switch after.Reason {
	case cardinalityv1alpha1.ReasonPaused, cardinalityv1alpha1.ReasonLimitReached, cardinalityv1alpha1.ReasonRateLimited:
	default:
		return nil
	}
	return &budgetEvent{Type: corev1.EventTypeWarning, Reason: after.Reason, Note: after.Message}
}
