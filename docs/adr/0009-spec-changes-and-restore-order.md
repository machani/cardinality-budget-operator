# 0009. Spec changes and restore order

- Status: Accepted
- Date: 2026-10-04

## Context

The design did not say what happens to existing drop rules when a budget's spec changes: mode lowered,
a dropped metric newly protected, the monitor selector changed, the cap lowered. New monitors created after a
drop were also unaddressed, and they look the same as drift ([ADR 0008](0008-drift-pause-and-resume.md)).

## Decision

**Principle**

- A person explicitly excludes something (protected metric, deselected monitor): remove **immediately**.
- The policy relaxes (mode lowered, cap lowered, limit raised): remove **gradually**, one step per settle period.
- Deleting the budget removes everything immediately through the finalizer
  `cardinality.telemetry-compass.com/cleanup`.

**Behaviour per change**

| Change | Behaviour | Speed |
| --- | --- | --- |
| `mode` Enforce to Warn or Observe | Restore all drops whatever the usage, `maxMetricsToDrop` per step | Gradual |
| Dropped metric now matches `protectedMetrics` | Restore it without waiting for the settle period; still set `lastEnforcementTime` | Immediate |
| Monitor no longer selected | Remove owned rules from it | Immediate |
| New monitor, or monitor newly selected | Extend active drops to it; not a new drop, not counted toward caps or the hourly limit; skipped while paused | Immediate |
| `maxTotalDrops` lowered below current count | Restore the excess | Gradual |
| `maxActiveSeries`, `warnThresholdPercent` | Normal phase logic and restore rule | Normal |
| `gracePeriod` | Applies at once (computed from `exceededSince`) | Immediate |
| Budget deleted | Finalizer removes all owned rules from all monitors | Immediate |

**Restore order**: most recently dropped first (LIFO).

**New monitor versus drift**

For an active drop, a selected monitor missing the rule is treated as:

1. **New monitor** if `metadata.creationTimestamp` is after the drop's `since`: extend the drop.
2. **Re-sync** if `metadata.generation != status.observedGeneration` (the platform edited the budget): extend
   to all selected monitors.
3. **Drift** otherwise: `Reverted` and pause.

A monitor deleted and recreated gets the drop again rather than being flagged as drift. Argo CD self-heal
patches objects rather than recreating them, so it is still detected.

## Consequences

- No list of monitor IDs in status.
- The kill switch blocks all of these writes ([ADR 0010](0010-rate-limit-and-kill-switch.md)).
- Tests: one per row of the table, plus the three new-monitor-versus-drift cases.
