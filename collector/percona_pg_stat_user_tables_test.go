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
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toDriverValues(values []any) []driver.Value {
	result := make([]driver.Value, len(values))
	for i, v := range values {
		result[i] = v
	}
	return result
}

var statUserTablesColumns = []string{
	"datname", "schemaname", "relname", "seq_scan", "seq_tup_read", "idx_scan", "idx_tup_fetch", "n_tup_ins",
	"n_tup_upd", "n_tup_del", "n_tup_hot_upd", "n_live_tup", "n_dead_tup", "n_mod_since_analyze", "last_vacuum",
	"last_autovacuum", "last_analyze", "last_autoanalyze", "vacuum_count", "autovacuum_count", "analyze_count",
	"autoanalyze_count", "indexes_size", "table_size",
}

func statUserTablesRow(extra ...any) []driver.Value {
	ts := time.Unix(1685059842, 0)
	row := []any{"postgres", "public", "t1", 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, ts, ts, ts, ts, 12, 13, 14, 15, 4096, 8192}
	return toDriverValues(append(row, extra...))
}

func TestStatUserTablesTimingsQuery(t *testing.T) {
	before18 := newStatUserTablesTimings(pg17).query(statUserTablesQuery)
	assert.Equal(t, statUserTablesQuery, before18)

	since18 := newStatUserTablesTimings(pg18).query(statUserTablesQuery)
	assert.Contains(t, strings.Join(strings.Fields(since18), " "),
		"pg_table_size(relid) as table_size, total_vacuum_time, total_autovacuum_time, total_analyze_time, total_autoanalyze_time FROM pg_stat_user_tables")
}

func TestPGStatUserTablesCollectorTotalSize(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(sanitizeQuery(statUserTablesQuery)).WillReturnRows(sqlmock.NewRows(statUserTablesColumns).AddRow(statUserTablesRow()...))

	inst := &instance{db: db, version: pg17}
	c := PGStatUserTablesCollector{}
	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return c.Update(context.Background(), inst, ch)
	})

	labels := labelMap{"datname": "postgres", "schemaname": "public", "relname": "t1"}
	// pg_total_relation_size, which the metric reported before, is the table with its indexes.
	assert.Equal(t, []MetricResult{{labels: labels, metricType: dto.MetricType_GAUGE, value: 12288}}, metrics["pg_stat_user_tables_size_bytes"])
	assert.NotContains(t, metrics, "pg_stat_user_tables_total_vacuum_time")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPGStatUserTablesCollectorTimings(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	columns := append(append([]string{}, statUserTablesColumns...),
		"total_vacuum_time", "total_autovacuum_time", "total_analyze_time", "total_autoanalyze_time")
	query := newStatUserTablesTimings(pg18).query(statUserTablesQuery)
	mock.ExpectQuery(sanitizeQuery(query)).WillReturnRows(sqlmock.NewRows(columns).AddRow(statUserTablesRow(1.5, 2.5, nil, 4.5)...))

	inst := &instance{db: db, version: pg18}
	c := PGStatUserTablesCollector{}
	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return c.Update(context.Background(), inst, ch)
	})

	labels := labelMap{"datname": "postgres", "schemaname": "public", "relname": "t1"}
	for name, value := range map[string]float64{
		"pg_stat_user_tables_total_vacuum_time":      1.5,
		"pg_stat_user_tables_total_autovacuum_time":  2.5,
		"pg_stat_user_tables_total_analyze_time":     0,
		"pg_stat_user_tables_total_autoanalyze_time": 4.5,
		"pg_stat_user_tables_size_bytes":             12288,
	} {
		require.Len(t, metrics[name], 1, name)
		assert.Equal(t, labels, metrics[name][0].labels, name)
		assert.InDelta(t, value, metrics[name][0].value, 0, name)
	}
	assert.Equal(t, dto.MetricType_COUNTER, metrics["pg_stat_user_tables_total_vacuum_time"][0].metricType)
	assert.NoError(t, mock.ExpectationsWereMet())
}
