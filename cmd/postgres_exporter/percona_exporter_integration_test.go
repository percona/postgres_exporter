//go:build integration

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
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/percona/postgres_exporter/config"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// customQueriesDir lays out the query files of the repository the way PMM Client installs them.
func customQueriesDir(t *testing.T, files ...string) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range files {
		src, err := os.Open(filepath.Join("..", "..", name))
		require.NoError(t, err)
		dst, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		_, err = io.Copy(dst, src)
		require.NoError(t, err)
		require.NoError(t, src.Close())
		require.NoError(t, dst.Close())
	}
	return dir
}

// newPMMHandler returns a handler configured with what pmm-managed passes to the exporter.
func newPMMHandler(t *testing.T, maxConnections int64, mutate ...func(*config.Config)) *perconaHandler {
	t.Helper()

	dsn := os.Getenv("DATA_SOURCE_NAME")
	require.NotEmpty(t, dsn, "DATA_SOURCE_NAME must point to a PostgreSQL server")

	return newTestHandler(t, false, func(cfg *config.Config) {
		cfg.DataSourceNames = strings.Split(dsn, ",")
		cfg.AutoDiscoverDatabases = true
		cfg.ExcludeDatabases = []string{"template0", "template1", "cloudsqladmin", "pmm-managed-dev", "azure_maintenance", "rdsadmin"}
		cfg.Percona.MaxConnections = maxConnections
		cfg.Percona.CustomQueriesHR = config.CustomQueriesConfig{Enabled: true, Directory: customQueriesDir(t, "queries-hr.yml", "queries-postgres-uptime.yml")}
		cfg.Percona.CustomQueriesMR = config.CustomQueriesConfig{Enabled: true, Directory: customQueriesDir(t, "queries-mr.yaml")}
		cfg.Percona.CustomQueriesLR = config.CustomQueriesConfig{Enabled: true, Directory: customQueriesDir(t, "queries-lr.yaml")}
		for _, m := range mutate {
			m(cfg)
		}
	})
}

func labelNames(family *dto.MetricFamily) []string {
	metrics := family.GetMetric()
	if len(metrics) == 0 {
		return nil
	}
	var names []string
	for _, l := range metrics[0].GetLabel() {
		names = append(names, l.GetName())
	}
	slices.Sort(names)
	return names
}

func TestPMMHighResolutionScrape(t *testing.T) {
	for _, maxConnections := range []int64{1, config.DefaultMaxConnections} {
		h := newPMMHandler(t, maxConnections)
		// The first scrape fills the caches the following ones rely on.
		scrape(t, h, "custom_query.hr", "exporter", "postgres", "standard.go", "standard.process")
		families := scrape(t, h, "custom_query.hr", "exporter", "postgres", "standard.go", "standard.process")

		require.Contains(t, families, "pg_up")
		assert.ElementsMatch(t, []string{"exporter", "custom_query.hr"}, labelValues(families["pg_up"], "collector"))
		for _, m := range families["pg_up"].GetMetric() {
			assert.InDelta(t, 1, m.GetGauge().GetValue(), 0)
		}

		// Metric families PMM dashboards and alerts query, with the labels they had before the exporter followed upstream.
		for name, labels := range map[string][]string{
			"pg_static":                                       {"server", "short_version", "version"},
			"pg_settings_max_connections":                     {"server"},
			"pg_stat_activity_count":                          {"application_name", "datname", "server", "state", "usename"},
			"pg_stat_activity_max_tx_duration":                {"application_name", "datname", "server", "state", "usename"},
			"pg_stat_activity_max_state_duration":             {"application_name", "datname", "server", "state", "usename"},
			"pg_stat_archiver_archived_count":                 {"server"},
			"pg_stat_bgwriter_checkpoints_timed":              {"collector", "server"},
			"pg_stat_bgwriter_checkpoints_timed_total":        {"collector", "server"},
			"pg_stat_bgwriter_buffers_backend":                {"collector", "server"},
			"pg_stat_bgwriter_checkpoint_sync_time":           {"collector", "server"},
			"pg_stat_database_xact_commit":                    {"datid", "datname"},
			"pg_stat_database_conflicts_confl_deadlock":       {"datid", "datname", "server"},
			"pg_locks_count":                                  {"datname", "mode"},
			"pg_database_size_bytes":                          {"datname"},
			"pg_database_wraparound_age_datfrozenxid_seconds": {"datname"},
			"pg_available_extensions":                         {"default_version", "installed_version", "name"},
			"pg_extensions":                                   {"name", "relocatable", "version"},
			"pg_replication_is_replica":                       nil,
			"pg_postmaster_uptime_seconds":                    {"server"},
			"pg_exporter_scrapes_total":                       {"collector"},
			"pg_scrape_collector_success":                     {"collector"},
		} {
			if assert.Contains(t, families, name) {
				assert.Equal(t, labels, labelNames(families[name]), name)
			}
		}

		// One source per metric: the upstream collectors that expose the same names stay off.
		assert.Len(t, families["pg_settings_max_connections"].GetMetric(), 1)
		assert.Len(t, families["pg_stat_archiver_archived_count"].GetMetric(), 1)

		for _, m := range families["pg_scrape_collector_success"].GetMetric() {
			assert.InDelta(t, 1, m.GetGauge().GetValue(), 0, m.GetLabel()[0].GetValue())
		}
		assert.ElementsMatch(t, []string{
			"database", "database_wraparound", "extensions", "locks", "replication", "replication_slot",
			"stat_bgwriter", "stat_database", "stat_user_tables", "statio_user_tables",
		}, labelValues(families["pg_scrape_collector_success"], "collector"))

		for _, m := range families["pg_exporter_user_queries_load_error"].GetMetric() {
			assert.Zero(t, m.GetGauge().GetValue())
		}
		assert.Contains(t, families, "go_goroutines")
	}
}

func TestDisableSettingsMetrics(t *testing.T) {
	h := newPMMHandler(t, config.DefaultMaxConnections, func(cfg *config.Config) {
		cfg.Percona.DisableSettingsMetrics = true
	})
	families := scrape(t, h, "exporter")

	assert.Contains(t, families, "pg_static")
	for name := range families {
		assert.False(t, strings.HasPrefix(name, "pg_settings_"), name)
	}
}

func TestPMMMediumAndLowResolutionScrapes(t *testing.T) {
	h := newPMMHandler(t, config.DefaultMaxConnections)

	for collect, metric := range map[string]string{
		"custom_query.mr": "pg_postmaster_start_time_seconds",
		"custom_query.lr": "pg_custom_database_size_custom_bytes",
	} {
		families := scrape(t, h, collect)

		require.Contains(t, families, "pg_up", collect)
		assert.Equal(t, []string{collect}, labelValues(families["pg_up"], "collector"))
		assert.InDelta(t, 1, families["pg_up"].GetMetric()[0].GetGauge().GetValue(), 0)
		for _, m := range families["pg_exporter_user_queries_load_error"].GetMetric() {
			assert.Zero(t, m.GetGauge().GetValue())
		}

		// Only the custom queries of the resolution run in these scrapes.
		assert.NotContains(t, families, "pg_static", collect)
		assert.NotContains(t, families, "pg_settings_max_connections", collect)
		assert.NotContains(t, families, "pg_scrape_collector_success", collect)
		assert.NotContains(t, families, "go_goroutines", collect)
		assert.Contains(t, families, metric, collect)
	}
}
