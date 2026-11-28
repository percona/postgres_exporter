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

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerconaPGLongRunningTransactionsCollector(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(sanitizeQuery(perconaLongRunningTransactionsQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"transactions", "oldest_timestamp_seconds"}).AddRow(20, 1200))

	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return PerconaPGLongRunningTransactionsCollector{}.Update(context.Background(), &instance{db: db}, ch)
	})

	assert.Equal(t, []MetricResult{{labels: labelMap{}, value: 20, metricType: dto.MetricType_GAUGE}}, metrics["pg_long_running_transactions"])
	assert.Equal(t, []MetricResult{{labels: labelMap{}, value: 1200, metricType: dto.MetricType_GAUGE}}, metrics["pg_long_running_transactions_oldest_timestamp_seconds"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPerconaLongRunningTransactionsQueryCountsEveryActiveTransaction(t *testing.T) {
	assert.NotContains(t, perconaLongRunningTransactionsQuery, "xact_start")
	assert.Contains(t, perconaLongRunningTransactionsQuery, "MAX(EXTRACT(EPOCH FROM clock_timestamp()))")
}
