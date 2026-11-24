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
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var statDatabaseColumns = []string{
	"datid", "datname", "numbackends", "xact_commit", "xact_rollback", "blks_read", "blks_hit", "tup_returned",
	"tup_fetched", "tup_inserted", "tup_updated", "tup_deleted", "conflicts", "temp_files", "temp_bytes", "deadlocks",
	"blk_read_time", "blk_write_time", "stats_reset", "active_time",
}

func statDatabaseRow(datname string, extra ...any) []driver.Value {
	row := []any{"1", datname, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15.0, 16.0, time.Unix(1685059842, 0), 17000.0}
	return toDriverValues(append(row, extra...))
}

func TestPGStatDatabaseCollectorParallelWorkers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	columns := append(append([]string{}, statDatabaseColumns...), "parallel_workers_to_launch", "parallel_workers_launched")
	// A row without the parallel worker statistics is skipped like any other incomplete row.
	rows := sqlmock.NewRows(columns).
		AddRow(statDatabaseRow("incomplete", nil, nil)...).
		AddRow(statDatabaseRow("postgres", 3, 2)...)
	mock.ExpectQuery(sanitizeQuery(statDatabaseQuery(columns))).WillReturnRows(rows)

	inst := &instance{db: db, version: pg18}
	c := PGStatDatabaseCollector{log: promslog.NewNopLogger()}
	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return c.Update(context.Background(), inst, ch)
	})

	labels := labelMap{"datid": "1", "datname": "postgres"}
	assert.Equal(t, []MetricResult{{labels: labels, metricType: dto.MetricType_COUNTER, value: 3}}, metrics["pg_stat_database_parallel_workers_to_launch"])
	assert.Equal(t, []MetricResult{{labels: labels, metricType: dto.MetricType_COUNTER, value: 2}}, metrics["pg_stat_database_parallel_workers_launched"])
	assert.Len(t, metrics["pg_stat_database_xact_commit"], 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPGStatDatabaseCollectorWithoutParallelWorkers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows(statDatabaseColumns).AddRow(statDatabaseRow("postgres")...)
	mock.ExpectQuery(sanitizeQuery(statDatabaseQuery(statDatabaseColumns))).WillReturnRows(rows)

	inst := &instance{db: db, version: pg17}
	c := PGStatDatabaseCollector{log: promslog.NewNopLogger()}
	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return c.Update(context.Background(), inst, ch)
	})

	assert.NotContains(t, metrics, "pg_stat_database_parallel_workers_to_launch")
	assert.NotContains(t, metrics, "pg_stat_database_parallel_workers_launched")
	assert.Len(t, metrics["pg_stat_database_xact_commit"], 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}
