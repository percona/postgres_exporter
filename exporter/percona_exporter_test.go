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
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/blang/semver/v4"
	"github.com/percona/postgres_exporter/distribution"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

func TestFingerprint(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"postgres://user:pass@db.example:6432/postgres?sslmode=disable", "db.example:6432"},
		{"postgresql://user:pass@db.example/postgres", "db.example:5432"},
		{"postgres://user:pass@/postgres?host=%2Fvar%2Frun%2Fpostgresql&sslmode=disable", "/var/run/postgresql:5432"},
		{"postgres://user:pass@/postgres?host=%2Fvar%2Frun%2Fpostgresql&port=6432", "/var/run/postgresql:6432"},
		{"host=db.example port=6432 user=u", "db.example:6432"},
		{"user=u dbname=postgres", "localhost:5432"},
	}
	for _, tt := range tests {
		got, err := Fingerprint(tt.dsn)
		require.NoError(t, err, tt.dsn)
		assert.Equal(t, tt.want, got, tt.dsn)
	}

	_, err := Fingerprint("not a dsn")
	assert.Error(t, err)
}

func TestNewServerLabelsSocketConnectionsWithTheSocketDirectory(t *testing.T) {
	server, err := NewServer("postgres://user:pass@/postgres?host=%2Fvar%2Frun%2Fpostgresql&sslmode=disable")
	require.NoError(t, err)
	defer server.Close()

	assert.Equal(t, "/var/run/postgresql:5432", server.String())
}

func TestDatabasePath(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"postgres://user:pass@db.example:5432/postgres", "postgres://user:pass@db.example:5432/other"},
		{"postgres://user:pass@/postgres?host=%2Fvar%2Frun%2Fpostgresql", "postgres://user:pass@/other?host=%2Fvar%2Frun%2Fpostgresql"},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.dsn)
		require.NoError(t, err)
		u.Path = databasePath(u, "other")
		assert.Equal(t, tt.want, u.String())
	}
}

func metricLabels(t *testing.T, m prometheus.Metric) map[string]string {
	t.Helper()
	pb := &dto.Metric{}
	require.NoError(t, m.Write(pb))
	labels := make(map[string]string, len(pb.Label))
	for _, l := range pb.Label {
		labels[l.GetName()] = l.GetValue()
	}
	return labels
}

func TestCollectorNameLabelsOnlyTheExportersOwnMetrics(t *testing.T) {
	e := NewExporter(nil, promslog.NewNopLogger(), WithConstantLabels("env=test"), CollectorName("custom_query.hr"))

	assert.Equal(t, map[string]string{"env": "test", "collector": "custom_query.hr"}, metricLabels(t, e.psqlUp))
	assert.Equal(t, map[string]string{"env": "test", "collector": "custom_query.hr"}, metricLabels(t, e.totalScrapes))

	server, err := NewServer("postgres://db.example:5432/postgres", e.servers.opts...)
	require.NoError(t, err)
	defer server.Close()
	assert.Equal(t, prometheus.Labels{"env": "test", "server": "db.example:5432"}, server.labels)
}

func TestExporterWithoutCollectorNameHasNoCollectorLabel(t *testing.T) {
	e := NewExporter(nil, promslog.NewNopLogger())
	assert.Empty(t, metricLabels(t, e.psqlUp))
}

func TestPerconaMetricMapsAreBuiltIn(t *testing.T) {
	for name := range perconaMetricMaps {
		assert.Contains(t, builtinMetricMaps, name)
		assert.Contains(t, queryOverrides, name)
	}
	for _, name := range []string{"pg_lock_conflicts", "pg_stat_replication", "pg_replication_slots", "pg_stat_archiver", "pg_stat_activity"} {
		assert.Contains(t, perconaMetricMaps, name)
	}
}

func TestMakeQueryOverrideMap(t *testing.T) {
	overrides := map[string][]OverrideQuery{
		"pg_versioned": {
			{semver.MustParseRange(">=10.0.0"), "new"},
			{semver.MustParseRange("<10.0.0"), "old"},
		},
		"pg_unsupported": {
			{semver.MustParseRange("<10.0.0"), "old"},
		},
	}

	got := makeQueryOverrideMap(semver.MustParse("17.0.0"), overrides, promslog.NewNopLogger(), "pg")
	assert.Equal(t, map[string]string{"pg_versioned": "new", "pg_unsupported": ""}, got)

	got = makeQueryOverrideMap(semver.MustParse("9.6.0"), overrides, promslog.NewNopLogger(), "custom")
	assert.Equal(t, map[string]string{"custom_versioned": "old", "custom_unsupported": "old"}, got)
}

func newTestServer() *Server {
	return &Server{
		labels:         prometheus.Labels{serverLabelName: "db.example:5432"},
		metricMap:      make(map[string]MetricMapNamespace),
		queryOverrides: make(map[string]string),
		logger:         promslog.NewNopLogger(),
	}
}

func TestLoadCustomQueries(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	write("first.yml", "pg_first:\n  query: \"SELECT 1 AS value\"\n  query_aurora: \"SELECT 2 AS value\"\n  metrics:\n    - value:\n        usage: \"GAUGE\"\n        description: \"first\"\n")
	write("second.yaml", "pg_second:\n  query: \"SELECT 3 AS value\"\n  query_aurora: \"!\"\n  metrics:\n    - value:\n        usage: \"COUNTER\"\n        description: \"second\"\n")
	write("broken.yml", "pg_broken: [")
	write("ignored.txt", "pg_ignored:\n  query: \"SELECT 4\"\n")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested.yml"), 0o700))

	loadErrors := func(e *Exporter) map[string]float64 {
		ch := make(chan prometheus.Metric, 10)
		e.userQueriesError.Collect(ch)
		close(ch)
		result := make(map[string]float64)
		for m := range ch {
			pb := &dto.Metric{}
			require.NoError(t, m.Write(pb))
			result[filepath.Base(metricLabels(t, m)["filename"])] = pb.GetGauge().GetValue()
		}
		return result
	}

	t.Run("standard", func(t *testing.T) {
		e := NewExporter(nil, promslog.NewNopLogger(), WithMetricPrefix("pg"), WithCustomQueriesDirectory(dir))
		server := newTestServer()
		e.loadCustomQueries(semver.MustParse("17.0.0"), server)

		assert.Equal(t, map[string]string{"pg_first": "SELECT 1 AS value", "pg_second": "SELECT 3 AS value"}, server.queryOverrides)
		assert.Contains(t, server.metricMap, "pg_first")
		assert.Contains(t, server.metricMap, "pg_second")
		assert.Equal(t, map[string]float64{"first.yml": 0, "second.yaml": 0, "broken.yml": 1}, loadErrors(e))
	})

	t.Run("aurora", func(t *testing.T) {
		e := NewExporter(nil, promslog.NewNopLogger(), WithMetricPrefix("pg"), WithCustomQueriesDirectory(dir))
		server := newTestServer()
		server.percona.distribution = distribution.Aurora
		e.loadCustomQueries(semver.MustParse("17.0.0"), server)

		assert.Equal(t, map[string]string{"pg_first": "SELECT 2 AS value"}, server.queryOverrides)
		assert.Contains(t, server.metricMap, "pg_first")
		assert.NotContains(t, server.metricMap, "pg_second")
	})

	t.Run("no directory", func(t *testing.T) {
		e := NewExporter(nil, promslog.NewNopLogger(), WithMetricPrefix("pg"))
		server := newTestServer()
		e.loadCustomQueries(semver.MustParse("17.0.0"), server)

		assert.Empty(t, server.queryOverrides)
		assert.Empty(t, loadErrors(e))
	})

	t.Run("missing directory", func(t *testing.T) {
		e := NewExporter(nil, promslog.NewNopLogger(), WithMetricPrefix("pg"), WithCustomQueriesDirectory(filepath.Join(dir, "missing")))
		server := newTestServer()
		e.loadCustomQueries(semver.MustParse("17.0.0"), server)

		assert.Empty(t, server.queryOverrides)
	})
}

func TestScrapeDSNsWaitsForAConnectionSlot(t *testing.T) {
	sem := semaphore.NewWeighted(1)
	require.True(t, sem.TryAcquire(1))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	// The DSNs point nowhere: the scrape has to give up before it tries to connect.
	dsns := []string{"postgres://127.0.0.1:1/a?sslmode=disable", "postgres://127.0.0.1:1/b?sslmode=disable"}
	e := NewExporter(dsns, promslog.NewNopLogger(), WithMetricPrefix("pg"), WithConnectionsSemaphore(sem))

	errorsCount, connectionErrorsCount := e.scrapeDSNs(ctx, make(chan prometheus.Metric), dsns)
	assert.Zero(t, errorsCount)
	assert.Zero(t, connectionErrorsCount)
}

// blockedExporter returns an exporter that waits for the only connection slot, which is taken,
// for as long as its scrape may run.
func blockedExporter(t *testing.T, opts ...ExporterOpt) *Exporter {
	t.Helper()

	sem := semaphore.NewWeighted(1)
	require.True(t, sem.TryAcquire(1))

	dsns := []string{"postgres://127.0.0.1:1/postgres?sslmode=disable"}
	return NewExporter(dsns, promslog.NewNopLogger(),
		append(opts, WithMetricPrefix("pg"), WithConnectionsSemaphore(sem), WithCollectionTimeout(10*time.Millisecond))...)
}

// collect returns a channel that is closed once the exporter has finished collecting.
func collect(e *Exporter) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ch := make(chan prometheus.Metric)
		go func() {
			defer close(ch)
			e.Collect(ch)
		}()
		for range ch {
		}
	}()
	return done
}

func TestScrapeContextIsTheOnlyLimitOfAScrape(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := collect(blockedExporter(t, WithContext(ctx)))

	select {
	case <-done:
		require.FailNow(t, "the collection timeout ended a running scrape")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the collection outlived its scrape")
	}
}

func TestCollectionTimeoutLimitsAScrapeWithoutScrapeContext(t *testing.T) {
	select {
	case <-collect(blockedExporter(t)):
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the collection outlived its collection timeout")
	}
}

// logTo returns a logger that writes logfmt lines to buf.
func logTo(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

// mockedServer returns a server whose database is mocked and whose custom queries are defined by queries.
func mockedServer(t *testing.T, queries string, logs *bytes.Buffer) (*Server, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	server := newTestServer()
	server.db = db
	server.logger = logTo(logs)
	require.NoError(t, addQueries([]byte(queries), semver.MustParse("17.0.0"), server, "pg"))
	return server, mock
}

const customQuery = `pg_custom:
  query: "SELECT name, value FROM custom"
  metrics:
    - name:
        usage: "LABEL"
        description: "Name."
    - value:
        usage: "GAUGE"
        description: "Value."
`

func TestQueryNamespaceMappingKeepsTheRowsReadBeforeAnError(t *testing.T) {
	var logs bytes.Buffer
	server, mock := mockedServer(t, customQuery, &logs)
	mock.ExpectQuery("SELECT name, value FROM custom").WillReturnRows(
		sqlmock.NewRows([]string{"name", "value"}).AddRow("a", 1).AddRow("b", 2).RowError(1, errors.New("connection lost")))

	metrics, nonFatal, err := queryNamespaceMapping(context.Background(), server, "pg_custom", server.metricMap["pg_custom"])
	require.NoError(t, err)
	assert.Empty(t, nonFatal)
	require.Len(t, metrics, 1)
	assert.Equal(t, "a", metricLabels(t, metrics[0])["name"])
}

func TestScrapeLogsEveryNamespaceError(t *testing.T) {
	var logs bytes.Buffer
	server, mock := mockedServer(t, customQuery, &logs)
	mock.ExpectQuery("SELECT name, value FROM custom").WillReturnError(errors.New("no such table"))

	err := server.Scrape(context.Background(), make(chan prometheus.Metric, 10))
	require.EqualError(t, err, "queryNamespaceMappings returned 1 errors")

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 3, logs.String())
	assert.Contains(t, lines[0], `level=ERROR msg="" err="Error running query on database`)
	assert.Contains(t, lines[1], `level=ERROR msg="NAMESPACE ERRORS FOUND"`)
	assert.Contains(t, lines[2], `level=ERROR msg="Error running query on database`)
	assert.Contains(t, lines[2], `namespace=pg_custom`)
}

func TestScrapeDSNsLogsTheErrorAlone(t *testing.T) {
	var logs bytes.Buffer
	dsns := []string{"postgres://user:secret@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1"}
	e := NewExporter(dsns, logTo(&logs), WithMetricPrefix("pg"))

	errorsCount, connectionErrorsCount := e.scrapeDSNs(context.Background(), make(chan prometheus.Metric, 100), dsns)
	assert.Equal(t, 1, errorsCount)
	assert.Equal(t, 1, connectionErrorsCount)

	assert.Contains(t, logs.String(), `level=ERROR msg="" err="Error opening connection to database`)
	assert.NotContains(t, logs.String(), "dsn=")
	assert.NotContains(t, logs.String(), "secret")
}

func TestAutoDiscoveryMakesTheLastDataSourceTheMaster(t *testing.T) {
	dsns := []string{
		"postgres://127.0.0.1:1/first?sslmode=disable&connect_timeout=1",
		"postgres://127.0.0.1:1/second?sslmode=disable&connect_timeout=1",
	}
	e := NewExporter(dsns, promslog.NewNopLogger(), WithMetricPrefix("pg"), AutoDiscoverDatabases(true))

	assert.ElementsMatch(t, dsns, e.discoverDatabaseDSNs(context.Background()))
	assert.Equal(t, dsns[1], e.percona.masterDSN)
}

func TestCustomQueriesErrorsAreLoggedWithThePath(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.yml")
	require.NoError(t, os.WriteFile(broken, []byte("pg_broken: ["), 0o600))

	var logs bytes.Buffer
	e := NewExporter(nil, logTo(&logs), WithMetricPrefix("pg"), WithCustomQueriesDirectory(dir))
	e.loadCustomQueries(semver.MustParse("17.0.0"), newTestServer())
	assert.Contains(t, logs.String(), `level=ERROR msg="Failed to reload user queries:`+broken+`" err=`)

	logs.Reset()
	missing := filepath.Join(dir, "missing")
	e = NewExporter(nil, logTo(&logs), WithMetricPrefix("pg"), WithCustomQueriesDirectory(missing))
	e.loadCustomQueries(semver.MustParse("17.0.0"), newTestServer())
	assert.Contains(t, logs.String(), `level=ERROR msg="failed read dir \"`+missing+`\" for custom query" err=`)
}
