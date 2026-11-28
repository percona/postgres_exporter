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

package exporter

import (
	"log/slog"
	"maps"
	"strings"

	"github.com/blang/semver/v4"
)

func init() {
	maps.Copy(builtinMetricMaps, perconaMetricMaps)
}

// perconaMetricMaps are the built-in metric maps PMM dashboards and alerts are built on.
var perconaMetricMaps = map[string]intermediateMetricMap{
	"pg_lock_conflicts": {
		map[string]ColumnMapping{
			"blocking_pid": {LABEL, "PID of blocking session", nil, nil},
			"count":        {GAUGE, "Number of blocked sessions", nil, nil},
		},
		true,
		0,
	},
	"pg_stat_replication": {
		map[string]ColumnMapping{
			"procpid":                  {DISCARD, "Process ID of a WAL sender process", nil, semver.MustParseRange("<9.2.0")},
			"pid":                      {DISCARD, "Process ID of a WAL sender process", nil, semver.MustParseRange(">=9.2.0")},
			"usesysid":                 {DISCARD, "OID of the user logged into this WAL sender process", nil, nil},
			"usename":                  {DISCARD, "Name of the user logged into this WAL sender process", nil, nil},
			"application_name":         {LABEL, "Name of the application that is connected to this WAL sender", nil, nil},
			"client_addr":              {LABEL, "IP address of the client connected to this WAL sender. If this field is null, it indicates that the client is connected via a Unix socket on the server machine.", nil, nil},
			"client_hostname":          {DISCARD, "Host name of the connected client, as reported by a reverse DNS lookup of client_addr. This field will only be non-null for IP connections, and only when log_hostname is enabled.", nil, nil},
			"client_port":              {DISCARD, "TCP port number that the client is using for communication with this WAL sender, or -1 if a Unix socket is used", nil, nil},
			"backend_start":            {DISCARD, "with time zone	Time when this process was started, i.e., when the client connected to this WAL sender", nil, nil},
			"backend_xmin":             {DISCARD, "The current backend's xmin horizon.", nil, nil},
			"state":                    {LABEL, "Current WAL sender state", nil, nil},
			"sent_location":            {DISCARD, "Last transaction log position sent on this connection", nil, semver.MustParseRange("<10.0.0")},
			"write_location":           {DISCARD, "Last transaction log position written to disk by this standby server", nil, semver.MustParseRange("<10.0.0")},
			"flush_location":           {DISCARD, "Last transaction log position flushed to disk by this standby server", nil, semver.MustParseRange("<10.0.0")},
			"replay_location":          {DISCARD, "Last transaction log position replayed into the database on this standby server", nil, semver.MustParseRange("<10.0.0")},
			"sent_lsn":                 {DISCARD, "Last transaction log position sent on this connection", nil, semver.MustParseRange(">=10.0.0")},
			"write_lsn":                {DISCARD, "Last transaction log position written to disk by this standby server", nil, semver.MustParseRange(">=10.0.0")},
			"flush_lsn":                {DISCARD, "Last transaction log position flushed to disk by this standby server", nil, semver.MustParseRange(">=10.0.0")},
			"replay_lsn":               {DISCARD, "Last transaction log position replayed into the database on this standby server", nil, semver.MustParseRange(">=10.0.0")},
			"sync_priority":            {DISCARD, "Priority of this standby server for being chosen as the synchronous standby", nil, nil},
			"sync_state":               {DISCARD, "Synchronous state of this standby server", nil, nil},
			"slot_name":                {LABEL, "A unique, cluster-wide identifier for the replication slot", nil, semver.MustParseRange(">=9.2.0")},
			"plugin":                   {DISCARD, "The base name of the shared object containing the output plugin this logical slot is using, or null for physical slots", nil, nil},
			"slot_type":                {DISCARD, "The slot type - physical or logical", nil, nil},
			"datoid":                   {DISCARD, "The OID of the database this slot is associated with, or null. Only logical slots have an associated database", nil, nil},
			"database":                 {DISCARD, "The name of the database this slot is associated with, or null. Only logical slots have an associated database", nil, nil},
			"active":                   {DISCARD, "True if this slot is currently actively being used", nil, nil},
			"active_pid":               {DISCARD, "Process ID of a WAL sender process", nil, nil},
			"xmin":                     {DISCARD, "The oldest transaction that this slot needs the database to retain. VACUUM cannot remove tuples deleted by any later transaction", nil, nil},
			"catalog_xmin":             {DISCARD, "The oldest transaction affecting the system catalogs that this slot needs the database to retain. VACUUM cannot remove catalog tuples deleted by any later transaction", nil, nil},
			"restart_lsn":              {DISCARD, "The address (LSN) of oldest WAL which still might be required by the consumer of this slot and thus won't be automatically removed during checkpoints", nil, nil},
			"pg_current_xlog_location": {DISCARD, "pg_current_xlog_location", nil, nil},
			"pg_current_wal_lsn":       {DISCARD, "pg_current_xlog_location", nil, semver.MustParseRange(">=10.0.0")},
			"pg_current_wal_lsn_bytes": {GAUGE, "WAL position in bytes", nil, semver.MustParseRange(">=10.0.0")},
			"pg_xlog_location_diff":    {GAUGE, "Lag in bytes between master and slave", nil, semver.MustParseRange(">=9.2.0 <10.0.0")},
			"pg_wal_lsn_diff":          {GAUGE, "Lag in bytes between master and slave", nil, semver.MustParseRange(">=10.0.0")},
			"confirmed_flush_lsn":      {DISCARD, "LSN position a consumer of a slot has confirmed flushing the data received", nil, nil},
			"write_lag":                {DISCARD, "Time elapsed between flushing recent WAL locally and receiving notification that this standby server has written it (but not yet flushed it or applied it). This can be used to gauge the delay that synchronous_commit level remote_write incurred while committing if this server was configured as a synchronous standby.", nil, semver.MustParseRange(">=10.0.0")},
			"flush_lag":                {DISCARD, "Time elapsed between flushing recent WAL locally and receiving notification that this standby server has written and flushed it (but not yet applied it). This can be used to gauge the delay that synchronous_commit level remote_flush incurred while committing if this server was configured as a synchronous standby.", nil, semver.MustParseRange(">=10.0.0")},
			"replay_lag":               {DISCARD, "Time elapsed between flushing recent WAL locally and receiving notification that this standby server has written, flushed and applied it. This can be used to gauge the delay that synchronous_commit level remote_apply incurred while committing if this server was configured as a synchronous standby.", nil, semver.MustParseRange(">=10.0.0")},
		},
		true,
		0,
	},
	"pg_replication_slots": {
		map[string]ColumnMapping{
			"slot_name":       {LABEL, "Name of the replication slot", nil, nil},
			"database":        {LABEL, "Name of the database", nil, nil},
			"active":          {GAUGE, "Flag indicating if the slot is active", nil, nil},
			"pg_wal_lsn_diff": {GAUGE, "Replication lag in bytes", nil, nil},
		},
		true,
		0,
	},
	"pg_stat_archiver": {
		map[string]ColumnMapping{
			"archived_count":     {COUNTER, "Number of WAL files that have been successfully archived", nil, nil},
			"last_archived_wal":  {DISCARD, "Name of the last WAL file successfully archived", nil, nil},
			"last_archived_time": {DISCARD, "Time of the last successful archive operation", nil, nil},
			"failed_count":       {COUNTER, "Number of failed attempts for archiving WAL files", nil, nil},
			"last_failed_wal":    {DISCARD, "Name of the WAL file of the last failed archival operation", nil, nil},
			"last_failed_time":   {DISCARD, "Time of the last failed archival operation", nil, nil},
			"stats_reset":        {DISCARD, "Time at which these statistics were last reset", nil, nil},
			"last_archive_age":   {GAUGE, "Time in seconds since last WAL segment was successfully archived", nil, nil},
		},
		true,
		0,
	},
	"pg_stat_activity": {
		map[string]ColumnMapping{
			"datname":            {LABEL, "Name of this database", nil, nil},
			"state":              {LABEL, "connection state", nil, semver.MustParseRange(">=9.2.0")},
			"usename":            {LABEL, "Name of the user logged into this backend", nil, nil},
			"application_name":   {LABEL, "Name of the application that is connected to this backend", nil, nil},
			"count":              {GAUGE, "number of connections in this state", nil, nil},
			"max_tx_duration":    {GAUGE, "max duration in seconds any active transaction has been running", nil, nil},
			"max_state_duration": {GAUGE, "max state change duration in seconds any active transaction has been", nil, nil},
		},
		true,
		0,
	},
}

// OverrideQuery 's are run in-place of simple namespace look ups, and provide
// advanced functionality. But they have a tendency to postgres version specific.
// There aren't too many versions, so we simply store customized versions using
// the semver matching we do for columns.
type OverrideQuery struct {
	versionRange semver.Range
	query        string
}

// Overriding queries for namespaces above.
// TODO: validate this is a closed set in tests, and there are no overlaps
var queryOverrides = map[string][]OverrideQuery{
	"pg_lock_conflicts": {
		{
			semver.MustParseRange(">0.0.0"),
			`SELECT blockinga.pid AS blocking_pid, count(*) as count
			FROM pg_catalog.pg_locks blockedl
			JOIN pg_stat_activity blockeda ON blockedl.pid = blockeda.pid
			JOIN pg_catalog.pg_locks blockingl ON(blockingl.transactionid=blockedl.transactionid
			  AND blockedl.pid != blockingl.pid)
			JOIN pg_stat_activity blockinga ON blockingl.pid = blockinga.pid
			WHERE NOT blockedl.granted
			group by blocking_pid`,
		},
	},
	"pg_stat_replication": {
		{
			semver.MustParseRange(">=10.0.0"),
			`
			SELECT *,
				(case pg_is_in_recovery() when 't' then null else pg_current_wal_lsn() end) AS pg_current_wal_lsn,
				(case pg_is_in_recovery() when 't' then null else pg_wal_lsn_diff(pg_current_wal_lsn(), pg_lsn('0/0'))::float end) AS pg_current_wal_lsn_bytes,
				(case pg_is_in_recovery() when 't' then null else pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn)::float end) AS pg_wal_lsn_diff
			FROM pg_stat_replication
			`,
		},
		{
			semver.MustParseRange(">=9.2.0 <10.0.0"),
			`
			SELECT *,
				(case pg_is_in_recovery() when 't' then null else pg_current_xlog_location() end) AS pg_current_xlog_location,
				(case pg_is_in_recovery() when 't' then null else pg_xlog_location_diff(pg_current_xlog_location(), replay_location)::float end) AS pg_xlog_location_diff
			FROM pg_stat_replication
			`,
		},
		{
			semver.MustParseRange("<9.2.0"),
			`
			SELECT *,
				(case pg_is_in_recovery() when 't' then null else pg_current_xlog_location() end) AS pg_current_xlog_location
			FROM pg_stat_replication
			`,
		},
	},

	"pg_replication_slots": {
		{
			semver.MustParseRange(">=9.4.0 <10.0.0"),
			`
			SELECT slot_name, database, active, pg_xlog_location_diff(pg_current_xlog_location(), restart_lsn)
			FROM pg_replication_slots
                        WHERE pg_is_in_recovery() = False
			`,
		},
		{
			semver.MustParseRange(">=10.0.0"),
			`
			SELECT slot_name, database, active, pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)
			FROM pg_replication_slots
                        WHERE pg_is_in_recovery() = False
			`,
		},
	},

	"pg_stat_archiver": {
		{
			semver.MustParseRange(">=9.4.0"),
			`
			SELECT *,
				extract(epoch from now() - last_archived_time) AS last_archive_age
			FROM pg_stat_archiver
			`,
		},
	},

	"pg_stat_activity": {
		// This query only works
		{
			semver.MustParseRange(">=9.2.0"),
			`
			SELECT
				pg_database.datname,
				tmp.state,
				tmp2.usename,
				tmp2.application_name,
				COALESCE(count,0) as count,
				COALESCE(max_tx_duration,0) as max_tx_duration,
				COALESCE(max_state_duration, 0) AS max_state_duration
			FROM
				(
				  VALUES ('active'),
				  		 ('idle'),
				  		 ('idle in transaction'),
				  		 ('idle in transaction (aborted)'),
				  		 ('fastpath function call'),
				  		 ('disabled')
				) AS tmp(state) CROSS JOIN pg_database
			LEFT JOIN
			(
				SELECT
					datname,
					state,
					usename,
					application_name,
					count(*) AS count,
					MAX(EXTRACT(EPOCH FROM now() - xact_start))::float AS max_tx_duration,
					MAX(EXTRACT(EPOCH FROM now() - state_change))::float AS max_state_duration
				FROM pg_stat_activity WHERE query not like 'autovacuum:%' GROUP BY datname,state,usename,application_name) AS tmp2
				ON tmp.state = tmp2.state AND pg_database.datname = tmp2.datname
			`,
		},
		{
			semver.MustParseRange("<9.2.0"),
			`
			SELECT
				datname,
				'unknown' AS state,
				usename,
				application_name,
				COALESCE(count(*),0) AS count,
				COALESCE(MAX(EXTRACT(EPOCH FROM now() - xact_start))::float,0) AS max_tx_duration,
				COALESCE(MAX(EXTRACT(EPOCH FROM now() - state_change))::float,0) AS max_state_duration
			FROM pg_stat_activity GROUP BY datname,usename,application_name
			`,
		},
	},
}

// makeQueryOverrideMap picks the query override matching the server version for every namespace.
func makeQueryOverrideMap(pgVersion semver.Version, queryOverrides map[string][]OverrideQuery, logger *slog.Logger, metricPrefix string) map[string]string {
	resultMap := make(map[string]string)
	for name, overrideDef := range queryOverrides {
		name = strings.Replace(name, "pg", metricPrefix, 1)

		// Find a matching semver. We make it an error to have overlapping
		// ranges at test-time, so only 1 should ever match.
		matched := false
		for _, queryDef := range overrideDef {
			if queryDef.versionRange(pgVersion) {
				resultMap[name] = queryDef.query
				matched = true
				break
			}
		}
		if !matched {
			logger.Warn("No query matched override, disabling metric space", "name", name)
			resultMap[name] = ""
		}
	}

	return resultMap
}
