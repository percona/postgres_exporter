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
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/blang/semver/v4"
	"github.com/percona/postgres_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerconaBGWriterIsTheStatBGWriterCollector(t *testing.T) {
	c, err := factories[config.CollectorStatBGWriter].create(collectorConfig{})
	require.NoError(t, err)
	assert.IsType(t, &PerconaPGStatBGWriterCollector{}, c)
}

func TestPerconaPGStatBGWriterCollector(t *testing.T) {
	statsReset := time.Unix(1685059842, 0)

	bgwriterBefore17 := []string{
		"checkpoints_timed", "checkpoints_req", "checkpoint_write_time", "checkpoint_sync_time", "buffers_checkpoint",
		"buffers_clean", "maxwritten_clean", "buffers_backend", "buffers_backend_fsync", "buffers_alloc", "stats_reset",
	}
	bgwriterAfter17 := []string{"buffers_clean", "maxwritten_clean", "buffers_alloc", "stats_reset"}
	checkpointer := []string{
		"num_timed", "num_requested", "num_done", "restartpoints_timed", "restartpoints_req", "restartpoints_done",
		"write_time", "sync_time", "buffers_written", "slru_written", "stats_reset",
	}

	tests := []struct {
		name    string
		version semver.Version
		expect  func(mock sqlmock.Sqlmock)
		want    map[string]float64
	}{
		{
			name:    "before PostgreSQL 17 everything comes from pg_stat_bgwriter",
			version: pg16,
			expect: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(sanitizeQuery(statBGWriterQueryBefore17)).WillReturnRows(sqlmock.NewRows(bgwriterBefore17).
					AddRow(354, 4945, 289097744.5, 1242257.25, int64(3275602074), 89320867, 450139, 2034563757, 7, int64(2725688749), statsReset))
			},
			want: map[string]float64{
				"checkpoints_timed": 354, "checkpoints_req": 4945, "checkpoint_write_time": 289097744.5,
				"checkpoint_sync_time": 1242257.25, "buffers_checkpoint": 3275602074, "buffers_clean": 89320867,
				"maxwritten_clean": 450139, "buffers_backend": 2034563757, "buffers_backend_fsync": 7,
				"buffers_alloc": 2725688749, "stats_reset": 1685059842,
			},
		},
		{
			name:    "PostgreSQL 17 reads the checkpoint columns from pg_stat_checkpointer",
			version: pg17,
			expect: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(sanitizeQuery(statBGWriterQueryAfter17)).WillReturnRows(sqlmock.NewRows(bgwriterAfter17).
					AddRow(11, 12, 13, statsReset))
				mock.ExpectQuery(sanitizeQuery(perconaCheckpointerQueryPrePG18)).WillReturnRows(sqlmock.NewRows(checkpointer).
					AddRow(21, 22, nil, 0, 0, 0, 23.5, 24.5, 25, nil, statsReset))
			},
			want: map[string]float64{
				"checkpoints_timed": 21, "checkpoints_req": 22, "checkpoint_write_time": 23.5,
				"checkpoint_sync_time": 24.5, "buffers_checkpoint": 25, "buffers_clean": 11,
				"maxwritten_clean": 12, "buffers_backend": 0, "buffers_backend_fsync": 0,
				"buffers_alloc": 13, "stats_reset": 1685059842,
			},
		},
		{
			name:    "PostgreSQL 18 adds checkpoints_done and slru_written",
			version: pg18,
			expect: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(sanitizeQuery(statBGWriterQueryAfter17)).WillReturnRows(sqlmock.NewRows(bgwriterAfter17).
					AddRow(11, 12, 13, statsReset))
				mock.ExpectQuery(sanitizeQuery(perconaCheckpointerQueryPostPG18)).WillReturnRows(sqlmock.NewRows(checkpointer).
					AddRow(21, 22, 20, 0, 0, 0, 23.5, 24.5, 25, 26, statsReset))
			},
			want: map[string]float64{
				"checkpoints_timed": 21, "checkpoints_req": 22, "checkpoints_done": 20, "checkpoint_write_time": 23.5,
				"checkpoint_sync_time": 24.5, "buffers_checkpoint": 25, "buffers_clean": 11,
				"maxwritten_clean": 12, "buffers_backend": 0, "buffers_backend_fsync": 0,
				"buffers_alloc": 13, "slru_written": 26, "stats_reset": 1685059842,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			tt.expect(mock)
			inst := &instance{db: db, dsn: testDSN, version: tt.version}

			metrics := collected(t, func(ch chan<- prometheus.Metric) error {
				return PerconaPGStatBGWriterCollector{}.Update(context.Background(), inst, ch)
			})

			labels := labelMap{"collector": "exporter", "server": testServer}
			want := make(map[string][]MetricResult, 2*len(tt.want))
			for name, value := range tt.want {
				result := []MetricResult{{labels: labels, metricType: dto.MetricType_COUNTER, value: value}}
				want["pg_stat_bgwriter_"+name] = result
				want["pg_stat_bgwriter_"+name+"_total"] = result
			}
			assert.Equal(t, want, metrics)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPerconaPGStatBGWriterCollectorNullValues(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	columns := []string{
		"checkpoints_timed", "checkpoints_req", "checkpoint_write_time", "checkpoint_sync_time", "buffers_checkpoint",
		"buffers_clean", "maxwritten_clean", "buffers_backend", "buffers_backend_fsync", "buffers_alloc", "stats_reset",
	}
	mock.ExpectQuery(sanitizeQuery(statBGWriterQueryBefore17)).WillReturnRows(sqlmock.NewRows(columns).
		AddRow(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))

	inst := &instance{db: db, dsn: testDSN, version: pg16}
	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return PerconaPGStatBGWriterCollector{}.Update(context.Background(), inst, ch)
	})

	require.Len(t, metrics, 22)
	for name, results := range metrics {
		require.Len(t, results, 1, name)
		assert.Zero(t, results[0].value, name)
	}
}
