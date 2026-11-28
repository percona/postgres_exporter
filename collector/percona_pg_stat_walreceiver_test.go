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
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// walReceiverMetrics runs the collector against rows with the given columns and returns its metrics in the order they are sent.
func walReceiverMetrics(t *testing.T, hasFlushedLSN bool, columns []string, row ...any) []MetricResult {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	columnRows := sqlmock.NewRows([]string{"column_name"})
	query := fmt.Sprintf(perconaPGStatWalReceiverQueryTemplate, "")
	if hasFlushedLSN {
		columnRows.AddRow("flushed_lsn")
		query = fmt.Sprintf(perconaPGStatWalReceiverQueryTemplate, "(flushed_lsn - '0/0') % (2^52)::bigint as flushed_lsn,\n")
	}
	mock.ExpectQuery(sanitizeQuery(perconaPGStatWalColumnQuery)).WillReturnRows(columnRows)
	values := make([]driver.Value, len(row))
	for i, v := range row {
		values[i] = v
	}
	mock.ExpectQuery(sanitizeQuery(query)).WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))

	ch := make(chan prometheus.Metric)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		c := PerconaPGStatWalReceiverCollector{log: promslog.NewNopLogger()}
		errCh <- c.Update(context.Background(), &instance{db: db}, ch)
	}()
	var metrics []MetricResult
	for m := range ch {
		metrics = append(metrics, readMetric(m))
	}
	require.NoError(t, <-errCh)
	assert.NoError(t, mock.ExpectationsWereMet())
	return metrics
}

func TestPerconaPGStatWalReceiverCollectorWithFlushedLSN(t *testing.T) {
	columns := []string{
		"upstream_host", "slot_name", "status", "receive_start_lsn", "receive_start_tli", "flushed_lsn",
		"received_tli", "last_msg_send_time", "last_msg_receipt_time", "latest_end_lsn", "latest_end_time", "upstream_node",
	}
	metrics := walReceiverMetrics(t, true, columns,
		"foo", "bar", "stopping", int64(1200668684563608), 1687321285, int64(1200668684563609),
		1687321280, 1687321275, 1687321276, int64(1200668684563610), 1687321277, 5)

	labels := labelMap{"upstream_host": "foo", "slot_name": "bar", "status": "stopping"}
	assert.Equal(t, []MetricResult{
		{labels: labels, value: 1200668684563608, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1687321285, metricType: dto.MetricType_GAUGE},
		{labels: labels, value: 1200668684563609, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1687321280, metricType: dto.MetricType_GAUGE},
		{labels: labels, value: 1687321275, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1687321276, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1200668684563610, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1687321277, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 5, metricType: dto.MetricType_GAUGE},
	}, metrics)
}

func TestPerconaPGStatWalReceiverCollectorWithNoFlushedLSN(t *testing.T) {
	columns := []string{
		"upstream_host", "slot_name", "status", "receive_start_lsn", "receive_start_tli",
		"received_tli", "last_msg_send_time", "last_msg_receipt_time", "latest_end_lsn", "latest_end_time", "upstream_node",
	}
	metrics := walReceiverMetrics(t, false, columns,
		"foo", "bar", "starting", int64(1200668684563608), 1687321285,
		1687321280, 1687321275, 1687321276, int64(1200668684563610), 1687321277, 5)

	labels := labelMap{"upstream_host": "foo", "slot_name": "bar", "status": "starting"}
	assert.Equal(t, []MetricResult{
		{labels: labels, value: 1200668684563608, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1687321285, metricType: dto.MetricType_GAUGE},
		{labels: labels, value: 1687321280, metricType: dto.MetricType_GAUGE},
		{labels: labels, value: 1687321275, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1687321276, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1200668684563610, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 1687321277, metricType: dto.MetricType_COUNTER},
		{labels: labels, value: 5, metricType: dto.MetricType_GAUGE},
	}, metrics)
}

func TestPerconaPGStatWalReceiverCollectorSkipsAReplicaWithoutUpstreamNode(t *testing.T) {
	columns := []string{
		"upstream_host", "slot_name", "status", "receive_start_lsn", "receive_start_tli", "flushed_lsn",
		"received_tli", "last_msg_send_time", "last_msg_receipt_time", "latest_end_lsn", "latest_end_time", "upstream_node",
	}
	metrics := walReceiverMetrics(t, true, columns,
		"foo", "standby1", "streaming", int64(1), 1, int64(2), 1, 1687321275, 1687321276, int64(3), 1687321277, nil)

	assert.Empty(t, metrics)
}

func TestPerconaPGStatWalReceiverCollectorSkipsAurora(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return PerconaPGStatWalReceiverCollector{log: promslog.NewNopLogger()}.Update(context.Background(), &instance{db: db, isAurora: true}, ch)
	})
	assert.Empty(t, metrics)
	assert.NoError(t, mock.ExpectationsWereMet())
}
