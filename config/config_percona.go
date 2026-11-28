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

// Percona collectors.
const (
	CollectorExtensions      = "extensions"
	CollectorReplicationSlot = "replication_slot"
	CollectorStatIO          = "stat_io"
)

// DefaultMaxConnections is the default number of connections the exporter opens to a database instance at once.
const DefaultMaxConnections int64 = 5

// PerconaConfig holds the settings PMM adds to the exporter.
type PerconaConfig struct {
	// DisableSettingsMetrics turns off the pg_settings metrics of the exporter's built-in maps.
	DisableSettingsMetrics bool
	// MaxConnections limits the connections a scrape of the metrics endpoint opens at once.
	MaxConnections int64
	// Custom queries are read from one directory per PMM scrape resolution.
	CustomQueriesHR CustomQueriesConfig
	CustomQueriesMR CustomQueriesConfig
	CustomQueriesLR CustomQueriesConfig
}

// CustomQueriesConfig describes the custom queries of one resolution.
type CustomQueriesConfig struct {
	Enabled   bool
	Directory string
}

// perconaCollectorConfig adds the Percona collectors to the upstream defaults and adjusts the defaults PMM relies on.
func perconaCollectorConfig(defaults map[string]bool) map[string]bool {
	defaults[CollectorExtensions] = true
	defaults[CollectorReplicationSlot] = true
	defaults[CollectorStatIO] = false

	defaults[CollectorDatabaseWraparound] = true
	defaults[CollectorWal] = false

	// The exporter's built-in metric maps and the replication_slot collector expose the same
	// metric names with the labels PMM expects, so their upstream counterparts are opt-in.
	defaults[CollectorReplicationSlots] = false
	defaults[CollectorSettings] = false
	defaults[CollectorStatActivity] = false
	defaults[CollectorStatArchiver] = false
	defaults[CollectorStatReplication] = false

	// pmm-managed can only turn off the collectors it knows, so the ones upstream added are opt-in.
	defaults[CollectorRoles] = false
	defaults[CollectorStatProgressVacuum] = false

	return defaults
}
