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
	"database/sql"
	"fmt"
	"time"

	"github.com/blang/semver/v4"
	"github.com/percona/postgres_exporter/exporter"
	"github.com/percona/postgres_exporter/internal/versioncache"
	"golang.org/x/sync/semaphore"
)

// perconaCollector holds the per-scrape state PMM passes to a PostgresCollector.
type perconaCollector struct {
	ctx      context.Context
	connSema *semaphore.Weighted
}

// WithContext sets the context that bounds a scrape.
func WithContext(ctx context.Context) Option {
	return func(p *PostgresCollector) error {
		p.percona.ctx = ctx
		return nil
	}
}

// WithConnectionsSemaphore sets the semaphore that limits the number of connections to the database instance.
func WithConnectionsSemaphore(sem *semaphore.Weighted) Option {
	return func(p *PostgresCollector) error {
		p.percona.connSema = sem
		return nil
	}
}

// collectionContext returns the context of a collection. The scrape timeout alone limits a scrape
// of the Percona handler; a collection without a scrape context is limited by timeout.
func (p perconaCollector) collectionContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if p.ctx == nil {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(p.ctx)
}

// acquireConnection waits for a free connection slot and returns the function that frees it.
func (p perconaCollector) acquireConnection(ctx context.Context) (func(), error) {
	if p.connSema == nil {
		return func() {}, nil
	}
	err := p.connSema.Acquire(ctx, 1)
	if err != nil {
		return nil, err
	}
	return func() { p.connSema.Release(1) }, nil
}

// serverName returns the host:port value of the server label.
func (i *instance) serverName() string {
	name, err := exporter.Fingerprint(i.dsn)
	if err != nil {
		return ""
	}
	return name
}

// cachedVersion returns the server version from the cache shared with the exporter package.
func (i *instance) cachedVersion(ctx context.Context) (semver.Version, error) {
	version, _, err := versioncache.GetVersion(i.db, i.serverName(), func(db *sql.DB, server string) (semver.Version, string, error) {
		version, err := queryVersion(ctx, db)
		if err != nil {
			return semver.Version{}, "", fmt.Errorf("error querying version on '%s': %w", server, err)
		}

		// The exporter package reports the full version string of a cached entry in pg_static.
		var versionString string
		err = db.QueryRowContext(ctx, "SELECT version();").Scan(&versionString)
		if err != nil {
			return semver.Version{}, "", fmt.Errorf("error scanning version string on '%s': %w", server, err)
		}
		return version, versionString, nil
	})
	return version, err
}
