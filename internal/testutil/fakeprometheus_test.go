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

package testutil

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// queryResponse is the part of a /api/v1/query answer these tests read.
type queryResponse struct {
	Status   string   `json:"status"`
	Warnings []string `json:"warnings"`
	Data     struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func postQuery(t *testing.T, base, query string) (int, queryResponse) {
	t.Helper()
	resp, err := http.PostForm(base+"/api/v1/query", url.Values{"query": {query}})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body queryResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, body
}

func TestReady(t *testing.T) {
	f := NewFakePrometheus()
	defer f.Close()

	for _, tc := range []struct {
		ready bool
		want  int
	}{{true, http.StatusOK}, {false, http.StatusServiceUnavailable}} {
		f.SetReady(tc.ready)
		resp, err := http.Get(f.URL() + "/-/ready")
		if err != nil {
			t.Fatalf("ready: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("ready=%v: status %d, want %d", tc.ready, resp.StatusCode, tc.want)
		}
	}
}

func TestRuntimeInfoUsesWholeSeconds(t *testing.T) {
	f := NewFakePrometheus()
	defer f.Close()
	f.SetRuntimeInfo(time.Now(), false, time.Date(2026, 10, 10, 13, 41, 20, 600_000_000, time.UTC))

	resp, err := http.Get(f.URL() + "/api/v1/status/runtimeinfo")
	if err != nil {
		t.Fatalf("runtimeinfo: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Data struct {
			ReloadConfigSuccess bool   `json:"reloadConfigSuccess"`
			LastConfigTime      string `json:"lastConfigTime"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.ReloadConfigSuccess {
		t.Error("reloadConfigSuccess = true, want false")
	}
	if body.Data.LastConfigTime != "2026-10-10T13:41:20Z" {
		t.Errorf("lastConfigTime = %q, want whole seconds", body.Data.LastConfigTime)
	}
}

func TestQueryResultsWarningsAndErrors(t *testing.T) {
	f := NewFakePrometheus()
	defer f.Close()
	const countQuery = `count({namespace="payments"})`
	f.SetResult(countQuery, Sample{Value: 61230})

	status, body := postQuery(t, f.URL(), countQuery)
	if status != http.StatusOK || len(body.Data.Result) != 1 || body.Data.Result[0].Value[1] != "61230" {
		t.Fatalf("known query: status %d, body %+v", status, body)
	}

	_, body = postQuery(t, f.URL(), `count({namespace="empty"})`)
	if len(body.Data.Result) != 0 {
		t.Errorf("unknown query returned %d samples, want an empty vector", len(body.Data.Result))
	}

	f.SetWarnings("partial response")
	_, body = postQuery(t, f.URL(), countQuery)
	if len(body.Warnings) != 1 {
		t.Errorf("warnings = %v, want one", body.Warnings)
	}

	f.SetQueryError(http.StatusServiceUnavailable, "down")
	status, body = postQuery(t, f.URL(), countQuery)
	if status != http.StatusServiceUnavailable || body.Status == "success" {
		t.Errorf("query error: status %d, body status %q", status, body.Status)
	}

	if got := strings.Join(f.Queries(), "|"); strings.Count(got, countQuery) != 3 {
		t.Errorf("recorded queries = %q, want the count query three times", got)
	}
}
