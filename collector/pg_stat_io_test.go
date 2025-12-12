// Copyright 2024 The Prometheus Authors
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
	"github.com/blang/semver/v4"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/smartystreets/goconvey/convey"
)

func TestPGStatIOCollector(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error opening a stub database connection: %s", err)
	}
	defer db.Close()

	inst := &instance{db: db, version: semver.MustParse("16.0.0")}

	columns := []string{"backend_type", "io_object", "io_context", "reads", "read_bytes", "read_time", "writes", "write_bytes", "write_time", "writebacks", "writeback_time", "extends", "extend_bytes", "extend_time", "hits", "evictions", "reueses", "fsyncs", "fsync_time"}
	rows := sqlmock.NewRows(columns).
		AddRow("client backend", "relation", "normal", 100, nil, 50.5, 75, nil, 25.2, 10, 12.0, 7, nil, 11.0, 1, 2, 3, 4, 8.0)
	mock.ExpectQuery("SELECT.*backend_type.*FROM pg_stat_io").WillReturnRows(rows)

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		c := PGStatIOCollector{}

		if err := c.Update(context.Background(), inst, ch); err != nil {
			t.Errorf("Error calling PGStatIOCollector.Update: %s", err)
		}
	}()

	labels := labelMap{"backend_type": "client backend", "io_object": "relation", "io_context": "normal"}
	expected := []MetricResult{
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 100},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 50.5},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 75},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 25.2},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 10},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 12.0},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 7},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 11.0},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 1},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 2},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 3},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 4},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 8.0},
	}

	convey.Convey("Metrics comparison", t, func() {
		for _, expect := range expected {
			m := readMetric(<-ch)
			convey.So(expect, convey.ShouldResemble, m)
		}
	})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled exceptions: %s", err)
	}
}

func TestPGStatIOCollectorPostgreSQL18(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error opening a stub database connection: %s", err)
	}
	defer db.Close()

	inst := &instance{db: db, version: pg18}

	columns := []string{"backend_type", "io_context", "io_object", "reads", "read_bytes", "read_time", "writes", "write_bytes", "write_time", "writebacks", "writeback_time", "extends", "extend_bytes", "extend_time", "hits", "evictions", "reueses", "fsyncs", "fsync_time"}
	rows := sqlmock.NewRows(columns).
		AddRow("client backend", "relation", "normal", 100, 90, 50.5, 75, 80, 25.2, 10, 12.0, 7, 30, 11.0, 1, 2, 3, 4, 8.0)
	mock.ExpectQuery("SELECT.*backend_type.*FROM pg_stat_io").WillReturnRows(rows)

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		c := PGStatIOCollector{}

		if err := c.Update(context.Background(), inst, ch); err != nil {
			t.Errorf("Error calling PGStatIOCollector.Update: %s", err)
		}
	}()

	labels := labelMap{"backend_type": "client backend", "io_object": "relation", "io_context": "normal"}
	expected := []MetricResult{
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 100},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 50.5},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 75},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 25.2},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 10},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 12.0},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 7},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 11.0},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 1},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 2},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 3},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 4},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 8.0},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 90},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 80},
		{labels: labels, metricType: dto.MetricType_COUNTER, value: 30},
	}

	convey.Convey("Metrics comparison", t, func() {
		for _, expect := range expected {
			m := readMetric(<-ch)
			convey.So(expect, convey.ShouldResemble, m)
		}
	})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled exceptions: %s", err)
	}
}

func TestPGStatIOCollectorPrePostgreSQL16(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error opening a stub database connection: %s", err)
	}
	defer db.Close()

	inst := &instance{db: db, version: semver.MustParse("15.0.0")}

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		c := PGStatIOCollector{}

		if err := c.Update(context.Background(), inst, ch); err != nil {
			t.Errorf("Error calling PGStatIOCollector.Update: %s", err)
		}
	}()

	// Should not make any queries for PostgreSQL < 16
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("There were unfulfilled expectations: %s", err)
	}

	for range ch {
		t.Error("Don't expect any metrics for PostgreSQL < 16")
	}
}
