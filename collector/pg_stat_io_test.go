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
)

func TestPGStatIOCollector(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error opening a stub database connection: %s", err)
	}
	defer db.Close()

	inst := &instance{db: db, version: semver.MustParse("16.0.0")}

	columns := []string{"backend_type", "io_context", "io_object", "reads", "read_time", "writes", "write_time", "extends", "read_bytes", "write_bytes", "extend_bytes"}
	rows := sqlmock.NewRows(columns).
		AddRow("client backend", "normal", "relation", 100, 50.5, 75, 25.2, 10, nil, nil, nil)
	mock.ExpectQuery("SELECT.*backend_type.*FROM pg_stat_io").WillReturnRows(rows)

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		c := PGStatIOCollector{}

		if err := c.Update(context.Background(), inst, ch); err != nil {
			t.Errorf("Error calling PGStatIOCollector.Update: %s", err)
		}
	}()

	expected := 5 // reads, read_time, writes, write_time, extends (no byte metrics for v16)

	metricCount := 0
	for m := range ch {
		metricCount++
		_ = m
	}

	if metricCount != expected {
		t.Errorf("Expected %d metrics, got %d", expected, metricCount)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("There were unfulfilled expectations: %s", err)
	}
}

func TestPGStatIOCollectorPostgreSQL18(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Error opening a stub database connection: %s", err)
	}
	defer db.Close()

	inst := &instance{db: db, version: semver.MustParse("18.0.0")}

	columns := []string{"backend_type", "io_context", "io_object", "reads", "read_time", "writes", "write_time", "extends", "read_bytes", "write_bytes", "extend_bytes"}
	rows := sqlmock.NewRows(columns).
		AddRow("client backend", "normal", "relation", 100, 50.5, 75, 25.2, 10, 1024, 2048, 512)
	mock.ExpectQuery("SELECT.*backend_type.*FROM pg_stat_io").WillReturnRows(rows)

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		c := PGStatIOCollector{}

		if err := c.Update(context.Background(), inst, ch); err != nil {
			t.Errorf("Error calling PGStatIOCollector.Update: %s", err)
		}
	}()

	expected := 8 // reads, read_time, writes, write_time, extends, read_bytes, write_bytes, extend_bytes

	metricCount := 0
	for m := range ch {
		metricCount++
		_ = m
	}

	if metricCount != expected {
		t.Errorf("Expected %d metrics, got %d", expected, metricCount)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("There were unfulfilled expectations: %s", err)
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

	metricCount := 0
	for m := range ch {
		metricCount++
		_ = m
	}

	if metricCount != 0 {
		t.Errorf("Expected 0 metrics for PostgreSQL < 16, got %d", metricCount)
	}
}
