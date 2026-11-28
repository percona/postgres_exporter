// Copyright 2026 Percona LLC
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alecthomas/kingpin/v2"
	"github.com/percona/postgres_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unreachableDSN makes every connection attempt fail at once.
const unreachableDSN = "postgres://user:pass@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1"

func newTestHandler(t *testing.T, disableExporterMetrics bool, mutate func(*config.Config)) *perconaHandler {
	t.Helper()

	cfg := config.NewConfigWithDefaults()
	cfg.DataSourceNames = []string{unreachableDSN}
	cfg.Percona.MaxConnections = config.DefaultMaxConnections
	if mutate != nil {
		mutate(&cfg)
	}
	validated, err := cfg.Validate()
	require.NoError(t, err)

	configMetrics := prometheus.NewRegistry()
	configMetrics.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{Name: "postgres_exporter_config_test", Help: "Stands in for the configuration file metrics."}))

	h, err := newPerconaHandler(promslog.NewNopLogger(), validated, configMetrics, disableExporterMetrics)
	require.NoError(t, err)
	return h
}

// scrape requests the collectors the way vmagent does and returns the metric families of the response.
func scrape(t *testing.T, h http.Handler, collect ...string) map[string]*dto.MetricFamily {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/metrics?"+url.Values{"collect[]": collect}.Encode(), nil)
	req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", "5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(rec.Body)
	require.NoError(t, err)
	return families
}

// labelValues returns the values of a label across the series of a metric family.
func labelValues(family *dto.MetricFamily, name string) []string {
	var values []string
	for _, m := range family.GetMetric() {
		for _, l := range m.GetLabel() {
			if l.GetName() == name {
				values = append(values, l.GetValue())
			}
		}
	}
	return values
}

func familyPrefixes(families map[string]*dto.MetricFamily) map[string]bool {
	prefixes := make(map[string]bool)
	for name := range families {
		prefix, _, _ := strings.Cut(name, "_")
		prefixes[prefix] = true
	}
	return prefixes
}

func TestNewFilters(t *testing.T) {
	assert.Equal(t, Filters{EnableAllCollectors: true}, newFilters(nil))
	assert.Equal(t, Filters{}, newFilters([]string{"unknown"}))
	assert.Equal(t, Filters{
		EnableDefaultCollector:  true,
		EnableHRCollector:       true,
		EnablePostgresCollector: true,
		EnableGoCollector:       true,
		EnableProcessCollector:  true,
	}, newFilters([]string{"custom_query.hr", "exporter", "postgres", "standard.go", "standard.process"}))
	assert.Equal(t, Filters{EnableMRCollector: true}, newFilters([]string{"custom_query.mr"}))
	assert.Equal(t, Filters{EnableLRCollector: true, EnableVersionCollector: true}, newFilters([]string{"custom_query.lr", "version"}))
}

func TestPerconaHandlerServesOnlyWhatIsCollected(t *testing.T) {
	enableCustomQueries := func(cfg *config.Config) {
		cfg.Percona.CustomQueriesHR.Enabled = true
		cfg.Percona.CustomQueriesMR.Enabled = true
	}

	t.Run("standard.go", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, false, nil), "standard.go")
		assert.Equal(t, map[string]bool{"go": true}, familyPrefixes(families))
	})

	t.Run("version comes with the configuration file metrics", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, false, nil), "version")
		assert.Len(t, families, 2)
		assert.Contains(t, families, "postgres_exporter_build_info")
		assert.Contains(t, families, "postgres_exporter_config_test")
	})

	t.Run("exporter", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, false, enableCustomQueries), "exporter")
		assert.Equal(t, map[string]bool{"pg": true}, familyPrefixes(families))
		require.Contains(t, families, "pg_up")
		assert.Equal(t, []string{"exporter"}, labelValues(families["pg_up"], "collector"))
		assert.Zero(t, families["pg_up"].GetMetric()[0].GetGauge().GetValue())
	})

	t.Run("custom queries of one resolution", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, false, enableCustomQueries), "custom_query.mr")
		assert.Equal(t, map[string]bool{"pg": true}, familyPrefixes(families))
		for name, family := range families {
			assert.Equal(t, []string{"custom_query.mr"}, labelValues(family, "collector"), name)
		}
	})

	t.Run("custom queries that are not enabled", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, false, nil), "custom_query.hr", "custom_query.mr", "custom_query.lr")
		assert.Empty(t, families)
	})

	t.Run("high resolution scrape of PMM", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, false, enableCustomQueries),
			"custom_query.hr", "exporter", "postgres", "standard.go", "standard.process")
		assert.Equal(t, map[string]bool{"pg": true, "go": true, "process": true}, familyPrefixes(families))
		require.Contains(t, families, "pg_up")
		assert.ElementsMatch(t, []string{"exporter", "custom_query.hr"}, labelValues(families["pg_up"], "collector"))
	})

	t.Run("everything", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, false, enableCustomQueries))
		assert.Equal(t, map[string]bool{"pg": true, "go": true, "process": true, "postgres": true}, familyPrefixes(families))
		assert.ElementsMatch(t, []string{"exporter", "custom_query.hr", "custom_query.mr"}, labelValues(families["pg_up"], "collector"))
		assert.Contains(t, families, "postgres_exporter_config_test")
	})

	t.Run("everything without the metrics of the exporter process", func(t *testing.T) {
		t.Parallel()
		families := scrape(t, newTestHandler(t, true, nil))
		assert.Equal(t, map[string]bool{"pg": true, "postgres": true}, familyPrefixes(families))
		assert.NotContains(t, families, "postgres_exporter_build_info")
	})

	t.Run("no data source", func(t *testing.T) {
		t.Parallel()
		h := newTestHandler(t, false, func(cfg *config.Config) { cfg.DataSourceNames = nil })
		families := scrape(t, h, "exporter", "postgres")
		assert.NotContains(t, families, "pg_scrape_collector_success")
		assert.Contains(t, families, "pg_up")
	})
}

func TestPerconaFlags(t *testing.T) {
	// Flags pmm-managed passes; a flag the exporter does not know keeps it from starting.
	for _, name := range []string{
		"collect.custom_query.hr", "collect.custom_query.mr", "collect.custom_query.lr",
		"collect.custom_query.hr.directory", "collect.custom_query.mr.directory", "collect.custom_query.lr.directory",
		"auto-discover-databases", "exclude-databases", "max-connections", "web.config", "disable-settings-metrics",
		"collector.database", "collector.database_wraparound", "collector.extensions", "collector.locks",
		"collector.replication", "collector.replication_slot", "collector.stat_bgwriter", "collector.stat_database",
		"collector.stat_user_tables", "collector.statio_user_tables", "collector.wal", "collector.stat_io",
	} {
		assert.NotNil(t, kingpin.CommandLine.GetFlag(name), name)
	}
}

func TestPerconaFlagDefaults(t *testing.T) {
	// The defaults of the fork. An upstream switch that changes what a scrape returns is off:
	// PMM gets the same from the exporter unless a flag says otherwise.
	for name, value := range map[string]string{
		"auto-discover-databases":           "false",
		"collect.custom_query.hr":           "false",
		"collect.custom_query.mr":           "false",
		"collect.custom_query.lr":           "false",
		"collect.custom_query.hr.directory": "",
		"collect.custom_query.mr.directory": "",
		"collect.custom_query.lr.directory": "",
		"config.file":                       "",
		"constantLabels":                    "",
		"disable-default-metrics":           "false",
		"disable-settings-metrics":          "false",
		"dumpmaps":                          "",
		"exclude-databases":                 "",
		"include-databases":                 "",
		"max-connections":                   "5",
		"metric-prefix":                     "pg",
		"web.config":                        "",
		"wrap-large-counters":               "false",
	} {
		flag := kingpin.CommandLine.GetFlag(name)
		if assert.NotNil(t, flag, name) {
			assert.Equal(t, value, strings.Join(flag.Model().Default, ","), name)
		}
	}
}

func TestPerconaConfigFromFlags(t *testing.T) {
	t.Cleanup(func() {
		_, err := kingpin.CommandLine.Parse(nil)
		require.NoError(t, err)
	})

	_, err := kingpin.CommandLine.Parse(nil)
	require.NoError(t, err)
	assert.Equal(t, config.PerconaConfig{MaxConnections: 5}, perconaConfig())

	_, err = kingpin.CommandLine.Parse([]string{
		"--collect.custom_query.hr", "--collect.custom_query.hr.directory=/hr",
		"--collect.custom_query.mr.directory=/mr",
		"--collect.custom_query.lr", "--collect.custom_query.lr.directory=/lr",
		"--max-connections=3", "--disable-settings-metrics", "--web.config=/web.yml",
	})
	require.NoError(t, err)
	assert.Equal(t, config.PerconaConfig{
		DisableSettingsMetrics: true,
		MaxConnections:         3,
		CustomQueriesHR:        config.CustomQueriesConfig{Enabled: true, Directory: "/hr"},
		CustomQueriesMR:        config.CustomQueriesConfig{Directory: "/mr"},
		CustomQueriesLR:        config.CustomQueriesConfig{Enabled: true, Directory: "/lr"},
	}, perconaConfig())

	cfg, err := buildConfig([]string{unreachableDSN})
	require.NoError(t, err)
	assert.Equal(t, perconaConfig(), cfg.Percona)

	file := "/upstream.yml"
	flags := &web.FlagConfig{WebConfigFile: &file}
	applyWebConfigAlias(flags)
	assert.Equal(t, "/web.yml", *flags.WebConfigFile)
}

func TestApplyWebConfigAliasKeepsWebConfigFile(t *testing.T) {
	file := "/upstream.yml"
	flags := &web.FlagConfig{WebConfigFile: &file}
	applyWebConfigAlias(flags)
	assert.Equal(t, "/upstream.yml", *flags.WebConfigFile)
}

func TestLargeCountersAreNotWrappedByDefault(t *testing.T) {
	t.Cleanup(func() {
		_, err := kingpin.CommandLine.Parse(nil)
		require.NoError(t, err)
	})

	_, err := kingpin.CommandLine.Parse(nil)
	require.NoError(t, err)
	cfg, err := buildConfig([]string{unreachableDSN})
	require.NoError(t, err)
	assert.False(t, cfg.WrapLargeCounters)

	_, err = kingpin.CommandLine.Parse([]string{"--wrap-large-counters"})
	require.NoError(t, err)
	cfg, err = buildConfig([]string{unreachableDSN})
	require.NoError(t, err)
	assert.True(t, cfg.WrapLargeCounters)
}

func TestExcludedDatabasesAreLoggedOnEveryScrape(t *testing.T) {
	cfg := config.NewConfigWithDefaults()
	cfg.DataSourceNames = []string{unreachableDSN}
	cfg.ExcludeDatabases = []string{"template0", "template1"}
	cfg.Percona.MaxConnections = config.DefaultMaxConnections
	validated, err := cfg.Validate()
	require.NoError(t, err)

	var logs bytes.Buffer
	h, err := newPerconaHandler(slog.New(slog.NewTextHandler(&logs, nil)), validated, prometheus.NewRegistry(), false)
	require.NoError(t, err)

	scrape(t, h, "custom_query.mr")
	scrape(t, h, "standard.go")
	assert.Equal(t, 2, strings.Count(logs.String(), `level=INFO msg="Excluded databases" databases="[template0 template1]"`), logs.String())
}
