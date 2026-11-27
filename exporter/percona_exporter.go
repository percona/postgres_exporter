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
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blang/semver/v4"
	"github.com/lib/pq"
	"github.com/percona/postgres_exporter/config"
	"github.com/percona/postgres_exporter/distribution"
	"github.com/percona/postgres_exporter/internal/versioncache"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/semaphore"
)

// collectorLabelName tells apart the exporters that share a registry in a single scrape.
const collectorLabelName = "collector"

// perconaExporter holds what PMM adds to an Exporter.
type perconaExporter struct {
	collectorName          string
	customQueriesDir       string
	disableSettingsMetrics bool
	connSema               *semaphore.Weighted
	ctx                    context.Context
	// masterDSNs are the configured data sources, as opposed to the discovered databases.
	masterDSNs []string
}

// perconaServer holds what PMM adds to a Server.
type perconaServer struct {
	distribution string
}

// CollectorName sets the value of the collector label on the exporter's own metrics.
func CollectorName(name string) ExporterOpt {
	return func(e *Exporter) {
		e.percona.collectorName = name
	}
}

// WithCustomQueriesDirectory sets the directory whose YAML files hold the custom queries to run.
func WithCustomQueriesDirectory(dir string) ExporterOpt {
	return func(e *Exporter) {
		e.percona.customQueriesDir = dir
	}
}

// WithContext sets the context that bounds a scrape.
func WithContext(ctx context.Context) ExporterOpt {
	return func(e *Exporter) {
		e.percona.ctx = ctx
	}
}

// WithConnectionsSemaphore sets the semaphore that limits the number of connections to the database instance.
func WithConnectionsSemaphore(sem *semaphore.Weighted) ExporterOpt {
	return func(e *Exporter) {
		e.percona.connSema = sem
	}
}

// DisableSettingsMetrics configures pg_settings export.
func DisableSettingsMetrics(b bool) ExporterOpt {
	return func(e *Exporter) {
		e.percona.disableSettingsMetrics = b
	}
}

// collectionContext returns the context of a scrape. The scrape timeout alone limits a scrape of
// the Percona handler; a scrape without a scrape context is limited by timeout.
func (p *perconaExporter) collectionContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if p.ctx == nil {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(p.ctx)
}

// internalLabels returns the constant labels of the exporter's own metrics.
func (p *perconaExporter) internalLabels(constantLabels prometheus.Labels) prometheus.Labels {
	if p.collectorName == "" {
		return constantLabels
	}
	labels := maps.Clone(constantLabels)
	if labels == nil {
		labels = make(prometheus.Labels)
	}
	labels[collectorLabelName] = p.collectorName
	return labels
}

// acquireConnection waits for a free connection slot and returns the function that frees it.
func (p *perconaExporter) acquireConnection(ctx context.Context) (func(), error) {
	if p.connSema == nil {
		return func() {}, nil
	}
	err := p.connSema.Acquire(ctx, 1)
	if err != nil {
		return nil, err
	}
	return func() { p.connSema.Release(1) }, nil
}

// Fingerprint returns the host:port value of the server label for a DSN.
func Fingerprint(dsn string) (string, error) {
	parsed, err := pq.ParseURL(dsn) //nolint:staticcheck
	if err != nil {
		parsed = dsn
	}

	pairs := strings.Split(parsed, " ")
	kv := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		splitted := strings.SplitN(pair, "=", 2)
		if len(splitted) != 2 {
			return "", fmt.Errorf("malformed dsn")
		}
		// pq.ParseURL quotes the values.
		key := strings.Trim(splitted[0], "'\"")
		value := strings.Trim(splitted[1], "'\"")
		kv[key] = value
	}

	host, ok := kv["host"]
	if !ok {
		host = "localhost"
	}
	port, ok := kv["port"]
	if !ok {
		port = "5432"
	}

	return host + ":" + port, nil
}

// openServer connects to dsn for the duration of a single scrape; the caller closes the server.
func (e *Exporter) openServer(ctx context.Context, dsn string) (*Server, error) {
	server, err := NewServers(e.servers.opts...).GetServer(ctx, dsn)
	if err != nil {
		return nil, err
	}
	server.percona.distribution = distribution.Get(dsn, server.db)
	return server, nil
}

// databaseNames returns the databases to scrape besides the one dsn points to.
func (e *Exporter) databaseNames(ctx context.Context, dsn string) ([]string, error) {
	release, err := e.percona.acquireConnection(ctx)
	if err != nil {
		e.logger.Warn("Failed to acquire semaphore", "err", err)
		return nil, err
	}
	defer release()

	server, err := e.openServer(ctx, dsn)
	if err != nil {
		e.logger.Error("Error opening connection to database", "dsn", loggableDSN(dsn), "err", config.RedactError(err, dsn))
		return nil, err
	}
	defer server.Close() //nolint:errcheck

	names, err := queryDatabases(ctx, server)
	if err != nil {
		e.logger.Error("Error querying databases", "dsn", loggableDSN(dsn), "err", config.RedactError(err, dsn))
		return nil, err
	}
	return names, nil
}

// databasePath returns the URL path that selects database, keeping the leading slash of a DSN without a host.
func databasePath(dsnURI *url.URL, database string) string {
	if dsnURI.Host == "" && strings.HasPrefix(dsnURI.Path, "/") {
		return "/" + database
	}
	return database
}

// scrapeDSNs scrapes every DSN on its own connection and returns the number of failed scrapes and
// how many of them failed to connect.
func (e *Exporter) scrapeDSNs(ctx context.Context, ch chan<- prometheus.Metric, dsns []string) (int, int) {
	var errorsCount, connectionErrorsCount atomic.Int32
	var wg sync.WaitGroup

	for _, dsn := range dsns {
		wg.Go(func() {
			release, err := e.percona.acquireConnection(ctx)
			if err != nil {
				e.logger.Warn("Failed to acquire semaphore", "err", err)
				return
			}
			defer release()

			err = e.scrapeDSN(ctx, ch, dsn)
			if err == nil {
				return
			}
			errorsCount.Add(1)
			e.logger.Error("error scraping dsn", "err", config.RedactError(err, dsn), "dsn", loggableDSN(dsn))

			if _, ok := err.(*ErrorConnectToServer); ok {
				connectionErrorsCount.Add(1)
			}
		})
	}
	wg.Wait()

	return int(errorsCount.Load()), int(connectionErrorsCount.Load())
}

// scrapeServer collects the settings and the namespace mappings of a server.
func (e *Exporter) scrapeServer(ctx context.Context, ch chan<- prometheus.Metric, server *Server) error {
	var settingsErr error
	if !e.percona.disableSettingsMetrics && server.master {
		err := querySettings(ctx, ch, server)
		if err != nil {
			settingsErr = fmt.Errorf("error retrieving settings: %w", err)
		}
	}

	err := server.Scrape(ctx, ch)
	if err != nil {
		return err
	}
	return settingsErr
}

// postgresVersion returns the server version, querying it at most once per versioncache.TTL.
func (e *Exporter) postgresVersion(ctx context.Context, server *Server) (semver.Version, string, error) {
	return versioncache.GetVersion(server.db, server.String(), func(db *sql.DB, name string) (semver.Version, string, error) {
		return checkPostgresVersion(ctx, db, name, e.logger)
	})
}

// loadCustomQueries adds the queries from every YAML file in the custom queries directory.
func (e *Exporter) loadCustomQueries(version semver.Version, server *Server) {
	dir := e.percona.customQueriesDir
	if dir == "" {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		e.logger.Error("Failed to read custom queries directory", "path", dir, "err", err)
		return
	}
	e.logger.Debug("Reading custom queries directory", "path", dir)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		e.addCustomQueriesFromFile(filepath.Join(dir, entry.Name()), version, server)
	}
}

func (e *Exporter) addCustomQueriesFromFile(path string, version semver.Version, server *Server) {
	userQueriesData, err := os.ReadFile(path)
	if err != nil {
		e.logger.Error("Failed to reload user queries", "path", path, "err", err)
		e.userQueriesError.WithLabelValues(path, "").Set(1)
		return
	}

	hashsumStr := fmt.Sprintf("%x", sha256.Sum256(userQueriesData))

	err = addQueries(userQueriesData, version, server, e.metricPrefix)
	if err != nil {
		e.logger.Error("Failed to reload user queries", "path", path, "err", err)
		e.userQueriesError.WithLabelValues(path, hashsumStr).Set(1)
		return
	}

	// Mark user queries as successfully loaded
	e.userQueriesError.WithLabelValues(path, hashsumStr).Set(0)
}
