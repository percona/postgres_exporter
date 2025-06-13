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

const statIOSubsystem = "stat_io"

func init() {
	registerCollector(statIOSubsystem, defaultDisabled, NewPGStatIOCollector)
}

type PGStatIOCollector struct {
	log log.Logger
}

func NewPGStatIOCollector(config collectorConfig) (Collector, error) {
	return &PGStatIOCollector{log: config.logger}, nil
}

var (
	statIOReads = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "reads_total"),
		"Number of reads",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOReadTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "read_time_seconds_total"),
		"Time spent reading",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWrites = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "writes_total"),
		"Number of writes",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWriteTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "write_time_seconds_total"),
		"Time spent writing",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOExtends = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "extends_total"),
		"Number of extends",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOReadBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "read_bytes_total"),
		"Number of bytes read (PostgreSQL 18+)",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWriteBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "write_bytes_total"),
		"Number of bytes written (PostgreSQL 18+)",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOExtendBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "extend_bytes_total"),
		"Number of bytes extended (PostgreSQL 18+)",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)

	// PostgreSQL 18+ query with byte statistics and WAL I/O
	StatIOQuery18Plus = `
		SELECT
			backend_type,
			io_context,
			io_object,
			reads,
			read_time,
			writes,
			write_time,
			extends,
			read_bytes,
			write_bytes,
			extend_bytes
		FROM pg_stat_io
	`

	// Pre-PostgreSQL 18 query without byte statistics
	StatIOQueryPre18 = `
		SELECT
			backend_type,
			io_context,
			io_object,
			reads,
			read_time,
			writes,
			write_time,
			extends,
			NULL::bigint as read_bytes,
			NULL::bigint as write_bytes,
			NULL::bigint as extend_bytes
		FROM pg_stat_io
	`
)

func (c *PGStatIOCollector) Update(ctx context.Context, instance *instance, ch chan<- prometheus.Metric) error {
	// pg_stat_io was introduced in PostgreSQL 16
	if instance.version.LT(semver.Version{Major: 16}) {
		return nil
	}

	db := instance.getDB()

	// Use version-specific query for PostgreSQL 18+
	query := StatIOQueryPre18
	if instance.version.GTE(semver.Version{Major: 18}) {
		query = StatIOQuery18Plus
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var backendType, ioContext, ioObject sql.NullString
		var reads, writes, extends, readBytes, writeBytes, extendBytes sql.NullFloat64
		var readTime, writeTime sql.NullFloat64

		err := rows.Scan(
			&backendType,
			&ioContext,
			&ioObject,
			&reads,
			&readTime,
			&writes,
			&writeTime,
			&extends,
			&readBytes,
			&writeBytes,
			&extendBytes,
		)
		if err != nil {
			return err
		}

		backendTypeLabel := "unknown"
		if backendType.Valid {
			backendTypeLabel = backendType.String
		}
		ioContextLabel := "unknown"
		if ioContext.Valid {
			ioContextLabel = ioContext.String
		}
		ioObjectLabel := "unknown"
		if ioObject.Valid {
			ioObjectLabel = ioObject.String
		}

		labels := []string{backendTypeLabel, ioContextLabel, ioObjectLabel}

		if reads.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOReads,
				prometheus.CounterValue,
				reads.Float64,
				labels...,
			)
		}

		if readTime.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOReadTime,
				prometheus.CounterValue,
				readTime.Float64/1000.0, // Convert milliseconds to seconds
				labels...,
			)
		}

		if writes.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOWrites,
				prometheus.CounterValue,
				writes.Float64,
				labels...,
			)
		}

		if writeTime.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOWriteTime,
				prometheus.CounterValue,
				writeTime.Float64/1000.0, // Convert milliseconds to seconds
				labels...,
			)
		}

		if extends.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOExtends,
				prometheus.CounterValue,
				extends.Float64,
				labels...,
			)
		}

		// PostgreSQL 18+ byte statistics
		if readBytes.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOReadBytes,
				prometheus.CounterValue,
				readBytes.Float64,
				labels...,
			)
		}

		if writeBytes.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOWriteBytes,
				prometheus.CounterValue,
				writeBytes.Float64,
				labels...,
			)
		}

		if extendBytes.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOExtendBytes,
				prometheus.CounterValue,
				extendBytes.Float64,
				labels...,
			)
		}
	}

	return nil
}
