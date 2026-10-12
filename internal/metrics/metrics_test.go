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

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/utils/ptr"
)

func TestBudgetSeriesAndDeletion(t *testing.T) {
	const ns = "payments"
	SetBudget(ns, BudgetState{Limit: 50000, ActiveSeries: ptr.To[int64](46000), Ready: true})

	if got := testutil.ToFloat64(usageRatio.WithLabelValues(ns)); got != 0.92 {
		t.Errorf("usage ratio = %v, want 0.92", got)
	}
	if got := testutil.ToFloat64(measurementValid.WithLabelValues(ns)); got != 1 {
		t.Errorf("measurement valid = %v, want 1", got)
	}

	DeleteBudget(ns)
	if n := testutil.CollectAndCount(limit); n != 0 {
		t.Errorf("limit series after delete = %d, want 0", n)
	}
}

func TestNoUsageBeforeFirstMeasurement(t *testing.T) {
	const ns = "new"
	SetBudget(ns, BudgetState{Limit: 1000})
	defer DeleteBudget(ns)
	if n := testutil.CollectAndCount(activeSeries); n != 0 {
		t.Errorf("active series reported before any measurement: %d series", n)
	}
}
