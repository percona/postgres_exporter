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
	"database/sql"

	"github.com/blang/semver/v4"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
)

const backendMemoryContextsSubsystem = "backend_memory_contexts"

func init() {
	registerCollector(backendMemoryContextsSubsystem, defaultDisabled, NewPGBackendMemoryContextsCollector)
}

type PGBackendMemoryContextsCollector struct {
	log log.Logger
}

func NewPGBackendMemoryContextsCollector(config collectorConfig) (Collector, error) {
	return &PGBackendMemoryContextsCollector{log: config.logger}, nil
}

var (
	backendMemoryContextsTotalBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, backendMemoryContextsSubsystem, "total_bytes"),
		"Total bytes allocated for memory context",
		[]string{"pid", "name", "ident", "parent", "level", "type", "path"},
		prometheus.Labels{},
	)
	backendMemoryContextsUsedBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, backendMemoryContextsSubsystem, "used_bytes"),
		"Used bytes in memory context",
		[]string{"pid", "name", "ident", "parent", "level", "type", "path"},
		prometheus.Labels{},
	)
	backendMemoryContextsFreeBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, backendMemoryContextsSubsystem, "free_bytes"),
		"Free bytes in memory context",
		[]string{"pid", "name", "ident", "parent", "level", "type", "path"},
		prometheus.Labels{},
	)
	backendMemoryContextsFreeChunks = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, backendMemoryContextsSubsystem, "free_chunks"),
		"Number of free chunks in memory context",
		[]string{"pid", "name", "ident", "parent", "level", "type", "path"},
		prometheus.Labels{},
	)

	// PostgreSQL 18+ query with type and path columns
	backendMemoryContextsQuery18Plus = `
		SELECT
			pid,
			name,
			COALESCE(ident, '') as ident,
			COALESCE(parent, '') as parent,
			level,
			total_bytes,
			total_nblocks,
			free_bytes,
			free_chunks,
			used_bytes,
			type,
			path
		FROM pg_backend_memory_contexts
		ORDER BY pid, name
	`

	// Pre-PostgreSQL 18 query without type and path columns
	backendMemoryContextsQueryPre18 = `
		SELECT
			pid,
			name,
			COALESCE(ident, '') as ident,
			COALESCE(parent, '') as parent,
			level,
			total_bytes,
			total_nblocks,
			free_bytes,
			free_chunks,
			used_bytes,
			'' as type,
			'' as path
		FROM pg_backend_memory_contexts
		ORDER BY pid, name
	`
)

func (c *PGBackendMemoryContextsCollector) Update(ctx context.Context, instance *instance, ch chan<- prometheus.Metric) error {
	// pg_backend_memory_contexts was introduced in PostgreSQL 14
	if instance.version.LT(semver.Version{Major: 14}) {
		return nil
	}

	db := instance.getDB()

	// Use version-specific query for PostgreSQL 18+
	query := backendMemoryContextsQueryPre18
	if instance.version.GTE(semver.Version{Major: 18}) {
		query = backendMemoryContextsQuery18Plus
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var pid, name, ident, parent, contextType, path sql.NullString
		var level, totalNblocks, freeChunks sql.NullInt64
		var totalBytes, freeBytes, usedBytes sql.NullFloat64

		err := rows.Scan(
			&pid,
			&name,
			&ident,
			&parent,
			&level,
			&totalBytes,
			&totalNblocks,
			&freeBytes,
			&freeChunks,
			&usedBytes,
			&contextType,
			&path,
		)
		if err != nil {
			return err
		}

		pidLabel := "unknown"
		if pid.Valid {
			pidLabel = pid.String
		}
		nameLabel := "unknown"
		if name.Valid {
			nameLabel = name.String
		}
		identLabel := ""
		if ident.Valid {
			identLabel = ident.String
		}
		parentLabel := ""
		if parent.Valid {
			parentLabel = parent.String
		}
		levelLabel := "0"
		if level.Valid {
			levelLabel = string(rune(level.Int64 + '0'))
		}
		typeLabel := ""
		if contextType.Valid {
			typeLabel = contextType.String
		}
		pathLabel := ""
		if path.Valid {
			pathLabel = path.String
		}

		labels := []string{pidLabel, nameLabel, identLabel, parentLabel, levelLabel, typeLabel, pathLabel}

		if totalBytes.Valid {
			ch <- prometheus.MustNewConstMetric(
				backendMemoryContextsTotalBytes,
				prometheus.GaugeValue,
				totalBytes.Float64,
				labels...,
			)
		}

		if usedBytes.Valid {
			ch <- prometheus.MustNewConstMetric(
				backendMemoryContextsUsedBytes,
				prometheus.GaugeValue,
				usedBytes.Float64,
				labels...,
			)
		}

		if freeBytes.Valid {
			ch <- prometheus.MustNewConstMetric(
				backendMemoryContextsFreeBytes,
				prometheus.GaugeValue,
				freeBytes.Float64,
				labels...,
			)
		}

		if freeChunks.Valid {
			ch <- prometheus.MustNewConstMetric(
				backendMemoryContextsFreeChunks,
				prometheus.GaugeValue,
				float64(freeChunks.Int64),
				labels...,
			)
		}
	}

	return nil
}
