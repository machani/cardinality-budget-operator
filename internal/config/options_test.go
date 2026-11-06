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

package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testURL = "http://kps-prometheus.monitoring.svc:9090"

// parsed returns Options as the flag package would fill them from args.
func parsed(t *testing.T, args ...string) *Options {
	t.Helper()
	o := &Options{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o.BindFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return o
}

// writeFile writes content to a file in a temporary directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsAreValid(t *testing.T) {
	o := parsed(t, "--prometheus-url="+testURL)
	if err := o.Validate(); err != nil {
		t.Fatalf("defaults with a URL should be valid: %v", err)
	}
	if o.MaxMeasurementAge != 2*DefaultMeasureInterval {
		t.Errorf("MaxMeasurementAge = %s, want 2 x measure interval", o.MaxMeasurementAge)
	}
	// The ADR 0013 default parses into two matchers on the job label.
	if len(o.ExcludeMatchers) != 2 || o.ExcludeMatchers[0].Name != "job" {
		t.Errorf("ExcludeMatchers = %v, want two job matchers", o.ExcludeMatchers)
	}
	if o.HTTPConfig != nil || len(o.Warnings) != 0 {
		t.Errorf("no HTTP config expected, got %v, warnings %v", o.HTTPConfig, o.Warnings)
	}
}

func TestInvalidFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing URL", nil, "--prometheus-url is required"},
		{"wrong scheme", []string{"--prometheus-url=ftp://prom:9090"}, "http or https"},
		{"no host", []string{"--prometheus-url=http://"}, "must include a host"},
		{"bad exclude-series", []string{"--prometheus-url=" + testURL, `--exclude-series=job=~"(`}, "--exclude-series"},
		{"zero interval", []string{"--prometheus-url=" + testURL, "--measure-interval=0s"}, "--measure-interval"},
		{"timeout too long", []string{"--prometheus-url=" + testURL, "--query-timeout=10m"}, "--query-timeout"},
		{"max age too short", []string{"--prometheus-url=" + testURL, "--max-measurement-age=1m"}, "--max-measurement-age"},
		{"negative warm-up", []string{"--prometheus-url=" + testURL, "--backend-warmup=-1m"}, "--backend-warmup"},
		{"zero settle", []string{"--prometheus-url=" + testURL, "--enforcement-settle-period=0s"}, "--enforcement-settle-period"},
		{"zero drops per hour", []string{"--prometheus-url=" + testURL, "--max-drops-per-hour=0"}, "--max-drops-per-hour"},
		{"empty namespace label", []string{"--prometheus-url=" + testURL, "--namespace-label="}, "--namespace-label"},
		{"missing http config file", []string{"--prometheus-url=" + testURL, "--prometheus-http-config=/does/not/exist"},
			"--prometheus-http-config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := parsed(t, tc.args...).Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestAllErrorsAreReportedTogether(t *testing.T) {
	err := parsed(t, "--measure-interval=0s", "--max-drops-per-hour=0").Validate()
	for _, want := range []string{"--prometheus-url", "--measure-interval", "--max-drops-per-hour"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %s", err, want)
		}
	}
}

func TestURLCredentialsAreRejectedAndNotEchoed(t *testing.T) {
	err := parsed(t, "--prometheus-url=https://admin:s3cret@prom.example:9090").Validate()
	if err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("error = %v, want a credentials error", err)
	}
	if strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "admin") {
		t.Errorf("error leaks the credentials: %v", err)
	}
}

func TestEmptyExcludeSeriesMeansNoExclusion(t *testing.T) {
	o := parsed(t, "--prometheus-url="+testURL, "--exclude-series=")
	if err := o.Validate(); err != nil || len(o.ExcludeMatchers) != 0 {
		t.Errorf("err = %v, matchers = %v; want no error and no matchers", err, o.ExcludeMatchers)
	}
}

func TestHTTPConfigWithSecretFiles(t *testing.T) {
	token := writeFile(t, "token", "abc")
	cfgPath := writeFile(t, "http.yaml", "authorization:\n  type: Bearer\n  credentials_file: "+token+"\n")
	o := parsed(t, "--prometheus-url="+testURL, "--prometheus-http-config="+cfgPath)
	if err := o.Validate(); err != nil {
		t.Fatalf("credentials_file should be accepted: %v", err)
	}
	if o.HTTPConfig == nil || o.HTTPConfig.Authorization.CredentialsFile != token {
		t.Errorf("HTTPConfig not loaded: %+v", o.HTTPConfig)
	}
}

func TestHTTPConfigInlineSecretsAreRejected(t *testing.T) {
	for name, content := range map[string]string{
		"basic_auth.password":       "basic_auth:\n  username: u\n  password: p4ss\n",
		"authorization.credentials": "authorization:\n  type: Bearer\n  credentials: t0ken\n",
	} {
		t.Run(name, func(t *testing.T) {
			o := parsed(t, "--prometheus-url="+testURL, "--prometheus-http-config="+writeFile(t, "http.yaml", content))
			err := o.Validate()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error = %v, want it to name %s", err, name)
			}
			if strings.Contains(err.Error(), "p4ss") || strings.Contains(err.Error(), "t0ken") {
				t.Errorf("error leaks the secret: %v", err)
			}
		})
	}
}

func TestInsecureSkipVerifyIsAWarning(t *testing.T) {
	cfgPath := writeFile(t, "http.yaml", "tls_config:\n  insecure_skip_verify: true\n")
	o := parsed(t, "--prometheus-url=https://prom.example:9090", "--prometheus-http-config="+cfgPath)
	if err := o.Validate(); err != nil {
		t.Fatalf("insecure_skip_verify should be allowed: %v", err)
	}
	if len(o.Warnings) != 1 {
		t.Errorf("warnings = %v, want one", o.Warnings)
	}
}

func TestDurationsAreParsed(t *testing.T) {
	o := parsed(t, "--prometheus-url="+testURL, "--measure-interval=1m", "--query-timeout=10s")
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	if o.MeasureInterval != time.Minute || o.MaxMeasurementAge != 2*time.Minute {
		t.Errorf("interval %s, max age %s", o.MeasureInterval, o.MaxMeasurementAge)
	}
}
