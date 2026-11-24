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

package collector

import (
	"database/sql"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	statUserTablesLabels = []string{"datname", "schemaname", "relname"}

	statUserTablesTotalSize = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, userTableSubsystem, "size_bytes"),
		"Total disk space used by this table, in bytes, including all indexes and TOAST data",
		statUserTablesLabels,
		prometheus.Labels{},
	)
	statUserTablesTotalVacuumTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, userTableSubsystem, "total_vacuum_time"),
		"Time spent vacuuming this table, in milliseconds (PostgreSQL 18+)",
		statUserTablesLabels,
		prometheus.Labels{},
	)
	statUserTablesTotalAutovacuumTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, userTableSubsystem, "total_autovacuum_time"),
		"Time spent auto-vacuuming this table, in milliseconds (PostgreSQL 18+)",
		statUserTablesLabels,
		prometheus.Labels{},
	)
	statUserTablesTotalAnalyzeTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, userTableSubsystem, "total_analyze_time"),
		"Time spent analyzing this table, in milliseconds (PostgreSQL 18+)",
		statUserTablesLabels,
		prometheus.Labels{},
	)
	statUserTablesTotalAutoanalyzeTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, userTableSubsystem, "total_autoanalyze_time"),
		"Time spent auto-analyzing this table, in milliseconds (PostgreSQL 18+)",
		statUserTablesLabels,
		prometheus.Labels{},
	)
)

// statUserTablesTimings reads the vacuum and analyze timing columns pg_stat_user_tables has since PostgreSQL 18.
type statUserTablesTimings struct {
	available                                bool
	vacuum, autovacuum, analyze, autoanalyze sql.NullFloat64
}

func newStatUserTablesTimings(version semver.Version) *statUserTablesTimings {
	return &statUserTablesTimings{available: version.GTE(semver.Version{Major: 18})}
}

// query adds the timing columns to the end of the select list.
func (t *statUserTablesTimings) query(query string) string {
	if !t.available {
		return query
	}
	from := strings.LastIndex(query, "FROM")
	return strings.TrimRight(query[:from], " \t\n") +
		",\n\t\ttotal_vacuum_time,\n\t\ttotal_autovacuum_time,\n\t\ttotal_analyze_time,\n\t\ttotal_autoanalyze_time\n\t" +
		query[from:]
}

func (t *statUserTablesTimings) scanTargets(targets ...any) []any {
	if !t.available {
		return targets
	}
	return append(targets, &t.vacuum, &t.autovacuum, &t.analyze, &t.autoanalyze)
}

func (t *statUserTablesTimings) emit(ch chan<- prometheus.Metric, labels ...string) {
	if !t.available {
		return
	}
	ch <- prometheus.MustNewConstMetric(statUserTablesTotalVacuumTime, prometheus.CounterValue, t.vacuum.Float64, labels...)
	ch <- prometheus.MustNewConstMetric(statUserTablesTotalAutovacuumTime, prometheus.CounterValue, t.autovacuum.Float64, labels...)
	ch <- prometheus.MustNewConstMetric(statUserTablesTotalAnalyzeTime, prometheus.CounterValue, t.analyze.Float64, labels...)
	ch <- prometheus.MustNewConstMetric(statUserTablesTotalAutoanalyzeTime, prometheus.CounterValue, t.autoanalyze.Float64, labels...)
}

// emitStatUserTablesTotalSize exposes what pg_total_relation_size reports: the table with its indexes.
func emitStatUserTablesTotalSize(ch chan<- prometheus.Metric, tableSize, indexSize float64, labels ...string) {
	ch <- prometheus.MustNewConstMetric(statUserTablesTotalSize, prometheus.GaugeValue, tableSize+indexSize, labels...)
}
