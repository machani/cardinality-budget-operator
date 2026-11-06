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

// Package config holds the operator's cluster-wide settings (HLD "Operator configuration"): the flags,
// their defaults, and the startup validation that stops the operator on an invalid value.
package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"time"
	"unicode/utf8"

	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// Defaults from the HLD table "Operator configuration" and ADR 0013.
const (
	DefaultNamespaceLabel          = "namespace"
	DefaultExcludeSeries           = `job!~"apiserver|coredns|kube-state-metrics|kubelet|node-exporter",job!=""`
	DefaultMeasureInterval         = 5 * time.Minute
	DefaultQueryTimeout            = 30 * time.Second
	DefaultBackendWarmup           = 15 * time.Minute
	DefaultEnforcementSettlePeriod = 10 * time.Minute
	DefaultMaxDropsPerHour         = 10
)

// Options are the operator flags. Call Validate after parsing; it also fills the derived fields.
type Options struct {
	PrometheusURL            string
	PrometheusHTTPConfigFile string
	NamespaceLabel           string
	ExcludeSeries            string
	MeasureInterval          time.Duration
	QueryTimeout             time.Duration
	BackendWarmup            time.Duration
	// MaxMeasurementAge 0 means 2 x MeasureInterval.
	MaxMeasurementAge       time.Duration
	EnforcementSettlePeriod time.Duration
	MaxDropsPerHour         int
	EnforcementPaused       bool

	// Derived by Validate.

	// ExcludeMatchers are the parsed --exclude-series matchers (empty when the flag is empty).
	ExcludeMatchers []*labels.Matcher
	// HTTPConfig is the loaded --prometheus-http-config, or nil when the flag is empty.
	HTTPConfig *commonconfig.HTTPClientConfig
	// Warnings are settings that are allowed but unsafe; main logs them at startup.
	Warnings []string
}

// BindFlags registers the flags on fs with their defaults.
func (o *Options) BindFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.PrometheusURL, "prometheus-url", "",
		"Base URL of the Prometheus 3.x HTTP API (required). Must not contain credentials.")
	fs.StringVar(&o.PrometheusHTTPConfigFile, "prometheus-http-config", "",
		"Path to a Prometheus-format HTTP client config file (auth, TLS, proxy). Secrets only as *_file references.")
	fs.StringVar(&o.NamespaceLabel, "namespace-label", DefaultNamespaceLabel,
		"Label that attributes a series to a namespace (ADR 0003).")
	fs.StringVar(&o.ExcludeSeries, "exclude-series", DefaultExcludeSeries,
		"PromQL label matchers excluding cluster exporters and recording-rule outputs from usage (ADR 0013).")
	fs.DurationVar(&o.MeasureInterval, "measure-interval", DefaultMeasureInterval,
		"Time between measurements.")
	fs.DurationVar(&o.QueryTimeout, "query-timeout", DefaultQueryTimeout,
		"Timeout per backend query.")
	fs.DurationVar(&o.BackendWarmup, "backend-warmup", DefaultBackendWarmup,
		"Minimum Prometheus uptime before a measurement is valid (ADR 0005).")
	fs.DurationVar(&o.MaxMeasurementAge, "max-measurement-age", 0,
		"Age after which a measurement is stale. 0 means 2 x --measure-interval (ADR 0005).")
	fs.DurationVar(&o.EnforcementSettlePeriod, "enforcement-settle-period", DefaultEnforcementSettlePeriod,
		"Wait between enforcement steps (ADR 0006).")
	fs.IntVar(&o.MaxDropsPerHour, "max-drops-per-hour", DefaultMaxDropsPerHour,
		"Global limit on new drops per hour across all budgets (ADR 0010).")
	fs.BoolVar(&o.EnforcementPaused, "enforcement-paused", false,
		"Kill switch: block every write to monitors (ADR 0010).")
}

// Validate checks every option and fills the derived fields. It returns all problems at once, so an
// operator with several bad flags is fixed in one go. Errors never contain credentials.
func (o *Options) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if err := validatePrometheusURL(o.PrometheusURL); err != nil {
		errs = append(errs, err)
	}

	if o.NamespaceLabel == "" || !utf8.ValidString(o.NamespaceLabel) {
		add("--namespace-label must be a non-empty UTF-8 label name")
	}

	o.ExcludeMatchers = nil
	if o.ExcludeSeries != "" {
		matchers, err := parser.NewParser(parser.Options{}).ParseMetricSelector("{" + o.ExcludeSeries + "}")
		if err != nil {
			add("--exclude-series is not a valid list of PromQL label matchers: %w", err)
		}
		o.ExcludeMatchers = matchers
	}

	if o.MeasureInterval <= 0 {
		add("--measure-interval must be positive, got %s", o.MeasureInterval)
	}
	if o.QueryTimeout <= 0 || o.QueryTimeout >= o.MeasureInterval {
		add("--query-timeout must be positive and shorter than --measure-interval, got %s", o.QueryTimeout)
	}
	if o.BackendWarmup < 0 {
		add("--backend-warmup must not be negative, got %s", o.BackendWarmup)
	}
	if o.MaxMeasurementAge == 0 {
		o.MaxMeasurementAge = 2 * o.MeasureInterval
	}
	if o.MaxMeasurementAge < o.MeasureInterval {
		add("--max-measurement-age must be at least --measure-interval, got %s", o.MaxMeasurementAge)
	}
	if o.EnforcementSettlePeriod <= 0 {
		add("--enforcement-settle-period must be positive, got %s", o.EnforcementSettlePeriod)
	}
	if o.MaxDropsPerHour < 1 {
		add("--max-drops-per-hour must be at least 1, got %d", o.MaxDropsPerHour)
	}

	o.HTTPConfig, o.Warnings = nil, nil
	if o.PrometheusHTTPConfigFile != "" {
		cfg, err := loadHTTPConfig(o.PrometheusHTTPConfigFile)
		if err != nil {
			errs = append(errs, err)
		} else {
			o.HTTPConfig = cfg
			if cfg.TLSConfig.InsecureSkipVerify {
				o.Warnings = append(o.Warnings,
					"--prometheus-http-config sets tls_config.insecure_skip_verify: the Prometheus certificate is not checked")
			}
		}
	}

	return errors.Join(errs...)
}

// validatePrometheusURL requires an http(s) URL with a host and no user info. Credentials belong in a
// mounted file referenced by --prometheus-http-config (ADR 0005), and the URL is logged, so it must not
// carry secrets. The URL itself is never put in the error.
func validatePrometheusURL(raw string) error {
	if raw == "" {
		return errors.New("--prometheus-url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("--prometheus-url is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("--prometheus-url must use http or https")
	}
	if u.Host == "" {
		return errors.New("--prometheus-url must include a host")
	}
	if u.User != nil {
		return errors.New("--prometheus-url must not contain credentials; use --prometheus-http-config with a credentials file")
	}
	return nil
}

// loadHTTPConfig loads and validates a Prometheus HTTP client config file and rejects inline secrets:
// credentials are only accepted as files mounted from Secrets (CLAUDE.md "Safety", ADR 0005).
func loadHTTPConfig(path string) (*commonconfig.HTTPClientConfig, error) {
	cfg, _, err := commonconfig.LoadHTTPConfigFile(path)
	if err != nil {
		return nil, fmt.Errorf("--prometheus-http-config %s: %w", path, err)
	}
	var inline []string
	if cfg.BasicAuth != nil && cfg.BasicAuth.Password != "" {
		inline = append(inline, "basic_auth.password")
	}
	if cfg.Authorization != nil && cfg.Authorization.Credentials != "" {
		inline = append(inline, "authorization.credentials")
	}
	if cfg.BearerToken != "" {
		inline = append(inline, "bearer_token")
	}
	if cfg.OAuth2 != nil && cfg.OAuth2.ClientSecret != "" {
		inline = append(inline, "oauth2.client_secret")
	}
	if cfg.TLSConfig.Key != "" {
		inline = append(inline, "tls_config.key")
	}
	if len(inline) > 0 {
		return nil, fmt.Errorf("--prometheus-http-config %s: inline secrets are not allowed (%v); use the *_file fields",
			path, inline)
	}
	return cfg, nil
}
