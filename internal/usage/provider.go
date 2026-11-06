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

// Package usage measures a namespace's active series in a metrics backend and decides whether the
// measurement can be trusted (HLD "Usage provider", ADR 0003, ADR 0005).
package usage

import (
	"context"
	"errors"
	"time"
)

// TopMetricsLimit is the number of metrics returned in Measurement.TopMetrics (status cap, HLD).
const TopMetricsLimit = 10

// MetricCount is the number of active series of one metric.
type MetricCount struct {
	Name   string
	Series int64
}

// Measurement is one valid reading of a namespace's usage.
type Measurement struct {
	// ActiveSeries is the number of active series in the namespace, after exclusions.
	ActiveSeries int64
	// TopMetrics are the metrics with the most series, largest first, at most TopMetricsLimit.
	TopMetrics []MetricCount
	// Time is when the measurement was taken.
	Time time.Time
}

// InvalidReason says why a measurement cannot be trusted. The values match the Ready condition reasons.
type InvalidReason string

const (
	// BackendUnreachable: the backend did not answer, or a query failed.
	BackendUnreachable InvalidReason = "BackendUnreachable"
	// BackendWarmingUp: the backend is not ready yet, or started less than --backend-warmup ago.
	BackendWarmingUp InvalidReason = "BackendWarmingUp"
	// PartialData: a query returned warnings, so results may be incomplete.
	PartialData InvalidReason = "PartialData"
)

// InvalidError is returned by Measure when the backend's answer cannot be trusted. The controller then
// sets phase Unknown and never adds or removes drop rules (ADR 0005).
type InvalidError struct {
	Reason  InvalidReason
	Message string
}

func (e *InvalidError) Error() string { return string(e.Reason) + ": " + e.Message }

// AsInvalid returns the InvalidError in err. Any other error is treated as BackendUnreachable, so an
// unexpected failure can never be mistaken for a valid measurement.
func AsInvalid(err error) *InvalidError {
	if invalid, ok := errors.AsType[*InvalidError](err); ok {
		return invalid
	}
	return &InvalidError{Reason: BackendUnreachable, Message: err.Error()}
}

// Provider measures namespace usage in a metrics backend.
type Provider interface {
	// Measure returns a valid measurement, or an error (usually *InvalidError) when the result cannot be
	// trusted. An empty result counts as 0 series only when every validity check passed.
	Measure(ctx context.Context, namespace string) (Measurement, error)

	// MetricSeries returns one metric's active series in the namespace, after exclusions. It is used by
	// the effectiveness check right after a valid Measure (ADR 0006).
	MetricSeries(ctx context.Context, namespace, metric string) (int64, error)

	// ConfigReloadedSince reports whether the backend has successfully loaded its configuration at or
	// after t, so a monitor change made at t is in effect (ADR 0006 reload check).
	ConfigReloadedSince(ctx context.Context, t time.Time) (bool, error)
}
