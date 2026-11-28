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
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/percona/postgres_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

func TestPerconaCollectorsAreRegistered(t *testing.T) {
	for _, name := range []string{config.CollectorExtensions, config.CollectorReplicationSlot, config.CollectorStatIO} {
		factory, ok := factories[name]
		require.True(t, ok, name)
		assert.Equal(t, serverScope, factory.scope, name)
	}
}

func TestPostgresCollectorDefaultsMatchPMM(t *testing.T) {
	p, err := NewPostgresCollector(promslog.NewNopLogger(), nil, testDSN, nil)
	require.NoError(t, err)

	names := make([]string, 0, len(p.Collectors))
	for name := range p.Collectors {
		names = append(names, name)
	}
	assert.ElementsMatch(t, []string{
		"database", "database_wraparound", "extensions", "locks", "replication", "replication_slot",
		"stat_bgwriter", "stat_database", "stat_user_tables", "statio_user_tables",
	}, names)
}

func TestInstanceServerName(t *testing.T) {
	assert.Equal(t, testServer, (&instance{dsn: testDSN}).serverName())
	assert.Equal(t, "/var/run/postgresql:5432", (&instance{dsn: "postgres://user:pass@/postgres?host=%2Fvar%2Frun%2Fpostgresql&sslmode=disable"}).serverName())
	assert.Empty(t, (&instance{dsn: "not a dsn"}).serverName())
}

func TestPerconaCollectorAcquireConnection(t *testing.T) {
	t.Run("no semaphore", func(t *testing.T) {
		release, err := perconaCollector{}.acquireConnection(context.Background())
		require.NoError(t, err)
		release()
	})

	t.Run("waits for a free slot until the scrape is cancelled", func(t *testing.T) {
		sem := semaphore.NewWeighted(1)
		p := perconaCollector{connSema: sem}

		release, err := p.acquireConnection(context.Background())
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err = p.acquireConnection(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		release()
		release, err = p.acquireConnection(context.Background())
		require.NoError(t, err)
		release()
	})
}

func TestPostgresCollectorCollectsNothingWithoutFreeConnection(t *testing.T) {
	sem := semaphore.NewWeighted(1)
	require.True(t, sem.TryAcquire(1))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	// The DSN points nowhere: the scrape has to give up before it tries to connect.
	p, err := NewPostgresCollector(logger, nil, "postgres://127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1", nil,
		WithContext(ctx), WithConnectionsSemaphore(sem))
	require.NoError(t, err)

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		p.Collect(ch)
	}()
	for m := range ch {
		t.Errorf("unexpected metric: %s", m.Desc())
	}
	assert.Contains(t, logs.String(), "Failed to acquire semaphore")
	assert.NotContains(t, logs.String(), "Error opening connection to database")
}

func TestPostgresCollectorReleasesItsConnectionSlot(t *testing.T) {
	sem := semaphore.NewWeighted(1)

	p, err := NewPostgresCollector(promslog.NewNopLogger(), nil, "postgres://127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1", nil,
		WithConnectionsSemaphore(sem))
	require.NoError(t, err)

	ch := make(chan prometheus.Metric)
	go func() {
		defer close(ch)
		p.Collect(ch)
	}()
	for range ch {
	}
	assert.True(t, sem.TryAcquire(1))
}

func TestPGStatWalReceiverCollectorSkipsAurora(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	c := PGStatWalReceiverCollector{log: promslog.NewNopLogger()}
	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		return c.Update(context.Background(), &instance{db: db, isAurora: true}, ch)
	})

	assert.Empty(t, metrics)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// blockedCollector returns a collector that waits for the only connection slot, which is taken,
// for as long as its collection may run.
func blockedCollector(t *testing.T, options ...Option) PostgresCollector {
	t.Helper()

	sem := semaphore.NewWeighted(1)
	require.True(t, sem.TryAcquire(1))

	p, err := NewPostgresCollector(promslog.NewNopLogger(), nil, "postgres://127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1", nil,
		append(options, WithConnectionsSemaphore(sem), WithCollectionTimeout("10ms"))...)
	require.NoError(t, err)
	return *p
}

func collect(p PostgresCollector) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Collect(make(chan prometheus.Metric))
	}()
	return done
}

func TestScrapeContextIsTheOnlyLimitOfACollection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := collect(blockedCollector(t, WithContext(ctx)))

	select {
	case <-done:
		require.FailNow(t, "the collection timeout ended the collection of a running scrape")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the collection outlived its scrape")
	}
}

func TestCollectionTimeoutLimitsACollectionWithoutScrapeContext(t *testing.T) {
	select {
	case <-collect(blockedCollector(t)):
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the collection outlived its collection timeout")
	}
}

func TestProbeRuntimeCollectsLikeTheMetricsEndpoint(t *testing.T) {
	cfg := config.NewConfigWithDefaults()
	cfg.DataSourceNames = []string{"postgres://127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1"}
	validated, err := cfg.Validate()
	require.NoError(t, err)

	sem := semaphore.NewWeighted(1)
	require.True(t, sem.TryAcquire(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var logs bytes.Buffer
	runtime, err := NewProbeRuntime(ctx, sem, validated, slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.Close() })

	registry := prometheus.NewRegistry()
	for _, c := range runtime.Collectors() {
		registry.MustRegister(c)
	}
	gathered := make(chan []*dto.MetricFamily, 1)
	go func() {
		families, err := registry.Gather()
		assert.NoError(t, err)
		gathered <- families
	}()
	var families []*dto.MetricFamily
	select {
	case families = <-gathered:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the collectors outlived their request")
	}

	// The only connection slot is taken and the request is gone: neither collector connects.
	assert.Equal(t, 2, strings.Count(logs.String(), "Failed to acquire semaphore"), logs.String())
	assert.NotContains(t, logs.String(), "127.0.0.1:1")

	// The exporter's own metrics are labelled as they are on the metrics endpoint.
	labelled := false
	for _, family := range families {
		if family.GetName() != "pg_up" {
			continue
		}
		for _, l := range family.GetMetric()[0].GetLabel() {
			labelled = labelled || (l.GetName() == "collector" && l.GetValue() == "exporter")
		}
	}
	assert.True(t, labelled, "pg_up has no collector label")
}

func TestProbeRuntimeQueriesTheTargetOnly(t *testing.T) {
	cfg := config.NewConfigWithDefaults()
	cfg.DataSourceNames = []string{testDSN}
	cfg.AutoDiscoverDatabases = true
	validated, err := cfg.Validate()
	require.NoError(t, err)

	runtime, err := NewProbeRuntime(context.Background(), nil, validated, promslog.NewNopLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.Close() })

	assert.Nil(t, runtime.postgresCollector.databaseDiscovery)
}

// partialCollector sends one metric and then fails.
type partialCollector struct{}

func (partialCollector) Update(_ context.Context, _ *instance, ch chan<- prometheus.Metric) error {
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc("pg_partial", "Sent before the error.", nil, nil), prometheus.GaugeValue, 1)
	return errors.New("failed after the first row")
}

func TestFailingCollectorKeepsTheMetricsItSent(t *testing.T) {
	metrics := collected(t, func(ch chan<- prometheus.Metric) error {
		execute(context.Background(), "partial", partialCollector{}, &instance{}, ch, promslog.NewNopLogger(), false)
		return nil
	})

	assert.Equal(t, []MetricResult{{labels: labelMap{}, value: 1, metricType: dto.MetricType_GAUGE}}, metrics["pg_partial"])
	assert.Equal(t, []MetricResult{{labels: labelMap{"collector": "partial"}, value: 0, metricType: dto.MetricType_GAUGE}}, metrics["pg_scrape_collector_success"])
}

func TestPerconaCollectorsReplaceUpstreams(t *testing.T) {
	for name, want := range map[string]Collector{
		"long_running_transactions": &PerconaPGLongRunningTransactionsCollector{},
		"stat_wal_receiver":         &PerconaPGStatWalReceiverCollector{},
	} {
		c, err := factories[name].create(collectorConfig{logger: promslog.NewNopLogger()})
		require.NoError(t, err, name)
		assert.IsType(t, want, c, name)
		assert.Equal(t, serverScope, factories[name].scope, name)
	}
}

func TestDatabaseWraparoundHelpOfTheSecondsMetrics(t *testing.T) {
	assert.Contains(t, databaseWraparoundAgeDatfrozenxidSeconds.String(), `help: "Age of the oldest transaction ID that has not been frozen."`)
	assert.Contains(t, databaseWraparoundAgeDatminmxidSeconds.String(), `help: "Age of the oldest multi-transaction ID that has been replaced with a transaction ID."`)
}
