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

// Package testutil holds helpers shared by tests. It is never imported by the operator itself.
package testutil

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Sample is one series in an instant-vector query result.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// FakePrometheus is an HTTP test server that answers the Prometheus API endpoints the operator uses:
// /-/ready, /api/v1/status/runtimeinfo and /api/v1/query (GET and POST). Tests change its state to
// simulate a backend that is healthy, not ready, warming up, failing to reload, returning warnings, or
// returning specific results (CLAUDE.md "Testing").
//
// By default it is ready, started an hour ago, has reloaded its config successfully, and answers every
// query with an empty vector, as Prometheus does for count() over no series.
type FakePrometheus struct {
	server *httptest.Server

	mu                  sync.Mutex
	ready               bool
	startTime           time.Time
	reloadConfigSuccess bool
	lastConfigTime      time.Time
	results             map[string][]Sample
	warnings            []string
	queryErrorStatus    int
	queryErrorMessage   string
	queries             []string
}

// NewFakePrometheus starts the server. Call Close when the test ends.
func NewFakePrometheus() *FakePrometheus {
	start := time.Now().Add(-time.Hour)
	f := &FakePrometheus{
		ready:               true,
		startTime:           start,
		reloadConfigSuccess: true,
		lastConfigTime:      start,
		results:             map[string][]Sample{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/-/ready", f.handleReady)
	mux.HandleFunc("/api/v1/status/runtimeinfo", f.handleRuntimeInfo)
	mux.HandleFunc("/api/v1/query", f.handleQuery)
	f.server = httptest.NewServer(mux)
	return f
}

// URL is the base URL to pass to the operator as --prometheus-url.
func (f *FakePrometheus) URL() string { return f.server.URL }

// Close stops the server. Requests after Close fail, which simulates an unreachable backend.
func (f *FakePrometheus) Close() { f.server.Close() }

// SetReady sets whether /-/ready answers 200 (true) or 503 (false).
func (f *FakePrometheus) SetReady(ready bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = ready
}

// SetRuntimeInfo sets the fields of /api/v1/status/runtimeinfo the operator reads (ADR 0005, ADR 0006).
// lastConfigTime is reported in whole seconds, like Prometheus 3.x (Phase 1 fact 5).
func (f *FakePrometheus) SetRuntimeInfo(startTime time.Time, reloadConfigSuccess bool, lastConfigTime time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startTime = startTime
	f.reloadConfigSuccess = reloadConfigSuccess
	f.lastConfigTime = lastConfigTime
}

// SetResult sets the instant-vector result returned for an exact query string. Other queries return an
// empty vector.
func (f *FakePrometheus) SetResult(query string, samples ...Sample) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[query] = samples
}

// SetWarnings sets the warnings returned with every successful query (partial data, ADR 0005).
func (f *FakePrometheus) SetWarnings(warnings ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warnings = warnings
}

// SetQueryError makes every query fail with the given HTTP status and message. Status 0 clears it.
func (f *FakePrometheus) SetQueryError(httpStatus int, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryErrorStatus = httpStatus
	f.queryErrorMessage = message
}

// Queries returns every query string received, in order.
func (f *FakePrometheus) Queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.queries)
}

func (f *FakePrometheus) handleReady(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	ready := f.ready
	f.mu.Unlock()
	if !ready {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("Prometheus Server is Ready.\n"))
}

func (f *FakePrometheus) handleRuntimeInfo(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	data := map[string]any{
		"startTime":           f.startTime.UTC().Format(time.RFC3339Nano),
		"reloadConfigSuccess": f.reloadConfigSuccess,
		"lastConfigTime":      f.lastConfigTime.UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, apiResponse(data, nil))
}

func (f *FakePrometheus) handleQuery(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	query := r.Form.Get("query")

	f.mu.Lock()
	f.queries = append(f.queries, query)
	errStatus, errMessage := f.queryErrorStatus, f.queryErrorMessage
	samples := slices.Clone(f.results[query])
	warnings := slices.Clone(f.warnings)
	f.mu.Unlock()

	if errStatus != 0 {
		writeJSON(w, errStatus, map[string]any{"status": "error", "errorType": "unavailable", "error": errMessage})
		return
	}

	now := float64(time.Now().Unix())
	result := make([]map[string]any, 0, len(samples))
	for _, s := range samples {
		labels := map[string]string{}
		maps.Copy(labels, s.Labels)
		result = append(result, map[string]any{
			"metric": labels,
			"value":  []any{now, strconv.FormatFloat(s.Value, 'f', -1, 64)},
		})
	}
	writeJSON(w, http.StatusOK, apiResponse(map[string]any{"resultType": "vector", "result": result}, warnings))
}

// apiResponse builds the success envelope of the Prometheus HTTP API.
func apiResponse(data any, warnings []string) map[string]any {
	body := map[string]any{"status": "success", "data": data}
	if len(warnings) > 0 {
		body["warnings"] = warnings
	}
	return body
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
