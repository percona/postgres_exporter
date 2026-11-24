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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPerconaCollectorDefaults(t *testing.T) {
	defaults := DefaultCollectorConfig()

	// The collectors pmm-managed expects to be on unless it passes --no-collector.<name>.
	for _, name := range []string{
		"database", "database_wraparound", "extensions", "locks", "replication", "replication_slot",
		"stat_bgwriter", "stat_database", "stat_user_tables", "statio_user_tables",
	} {
		assert.True(t, defaults[name], name)
	}

	// Their metrics come from the exporter's built-in maps and the replication_slot collector.
	for _, name := range []string{"replication_slots", "settings", "stat_activity", "stat_archiver", "stat_replication"} {
		enabled, ok := defaults[name]
		assert.True(t, ok, name)
		assert.False(t, enabled, name)
	}

	for _, name := range []string{"stat_io", "wal"} {
		enabled, ok := defaults[name]
		assert.True(t, ok, name)
		assert.False(t, enabled, name)
	}
}

func TestPMMCanDisableEveryDefaultCollector(t *testing.T) {
	// defaultPostgresExporterCollectors of pmm-managed: the collectors it passes --no-collector.<name> for.
	known := []string{
		"database", "database_wraparound", "extensions", "locks", "replication", "replication_slot",
		"stat_bgwriter", "stat_database", "stat_user_tables", "statio_user_tables", "wal",
	}

	for name, enabled := range DefaultCollectorConfig() {
		if enabled {
			assert.Contains(t, known, name)
		}
	}
}

func TestPerconaConfigSurvivesValidation(t *testing.T) {
	cfg := NewConfigWithDefaults()
	cfg.Percona = PerconaConfig{
		DisableSettingsMetrics: true,
		MaxConnections:         3,
		CustomQueriesHR:        CustomQueriesConfig{Enabled: true, Directory: "/hr"},
	}

	validated, err := cfg.Validate()
	assert.NoError(t, err)
	assert.Equal(t, cfg.Percona, validated.Config().Percona)
}
