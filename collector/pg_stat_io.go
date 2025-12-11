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
		"Number of read operations",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOReadBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "read_bytes_total"),
		"The total size of read operations, in bytes",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOReadTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "read_time_total"),
		"Time spent waiting for read operations, in milliseconds",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWrites = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "writes_total"),
		"Number of write operations",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWriteBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "write_bytes_total"),
		"The total size of write operations, in bytes",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWriteTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "write_time_total"),
		"Time spent waiting for write operations, in milliseconds",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWritebacks = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "writebacks_total"),
		"Number of units of size BLCKSZ (typically 8kB) which the process requested the kernel write out to permanent storage",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOWritebackTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "writeback_time_total"),
		"Time spent waiting for writeback operations, in milliseconds",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOExtends = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "extends_total"),
		"Number of extend operations",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOExtendBytes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "extend_bytes_total"),
		"The total size of extend operations, in bytes",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOExtendTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "extend_time_total"),
		"Time spent waiting for extend operations, in milliseconds",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOHits = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "hits_total"),
		"The number of times a desired block was found in a shared buffer",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOEvictions = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "evictions_total"),
		"Number of times a block has been written out from a shared or local buffer in order to make it available for another use",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOReueses = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "reueses_total"),
		"The number of times an existing buffer in a size-limited ring buffer outside of shared buffers was reused as part of an I/O operation in the bulkread, bulkwrite, or vacuum contexts",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOFsyncs = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "fsyncs_total"),
		"Number of fsync calls",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)
	statIOFsyncTime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statIOSubsystem, "fsync_time_total"),
		"Time spent waiting for fsync operations, in milliseconds",
		[]string{"backend_type", "io_context", "io_object"},
		prometheus.Labels{},
	)

	statIOQueryPrePG18 = `
		SELECT
			backend_type,
			object,
			context,
			reads,
			NULL:bigint as read_bytes,
			read_time,
			writes,
			NULL:bigint as write_bytes,
			write_time,
			writebacks,
			writeback_time,
			extends,
			NULL:numeric as extend_bytes,
			extend_time,
			hits,
			evictions,
			reuses,
			fsyncs,
			fsync_time
		FROM pg_stat_io;`

	statIOQueryPostPG18 = `
		SELECT
			backend_type,
			object,
			context,
			reads,
			read_bytes,
			read_time,
			writes,
			write_bytes,
			write_time,
			writebacks,
			writeback_time,
			extends,
			extend_bytes,
			extend_time,
			hits,
			evictions,
			reuses,
			fsyncs,
			fsync_time
		FROM pg_stat_io;`
)

func (c *PGStatIOCollector) Update(ctx context.Context, instance *instance, ch chan<- prometheus.Metric) error {
	// pg_stat_io was introduced in PostgreSQL 16
	if instance.version.LT(semver.Version{Major: 16}) {
		return nil
	}

	db := instance.getDB()

	v18plus := instance.version.GTE(semver.Version{Major: 18})

	statIOQuery := statIOQueryPrePG18
	if v18plus {
		statIOQuery = statIOQueryPostPG18
	}

	rows, err := db.QueryContext(ctx, statIOQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var backendType, ioContext, ioObject sql.NullString
		var reads, writes, writebacks, extends, hits, evictions, reueses, fsyncs sql.NullInt64
		var readBytes, writeBytes, extendBytes, readTime, writeTime, extendTime, writebackTime, fsyncTime sql.NullFloat64

		err := rows.Scan(
			&backendType,
			&ioObject,
			&ioContext,
			&reads,
			&readBytes,
			&readTime,
			&writes,
			&writeBytes,
			&writeTime,
			&writebacks,
			&writebackTime,
			&extends,
			&extendBytes,
			&extendTime,
			&hits,
			&evictions,
			&reueses,
			&fsyncs,
			&fsyncTime,
		)
		if err != nil {
			return err
		}

		backendTypeLabel := "unknown"
		if backendType.Valid {
			backendTypeLabel = backendType.String
		}
		ioObjectLabel := "unknown"
		if ioObject.Valid {
			ioObjectLabel = ioObject.String
		}
		ioContextLabel := "unknown"
		if ioContext.Valid {
			ioContextLabel = ioContext.String
		}

		labels := []string{backendTypeLabel, ioContextLabel, ioObjectLabel}

		readsMetric := 0.0
		if reads.Valid {
			readsMetric = float64(reads.Int64)
		}
		ch <- prometheus.MustNewConstMetric(
			statIOReads,
			prometheus.CounterValue,
			readsMetric,
			labels...,
		)

		if readTime.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOReadTime,
				prometheus.CounterValue,
				readTime.Float64,
				labels...,
			)
		}

		writesMetric := 0.0
		if writes.Valid {
			writesMetric = float64(writes.Int64)
		}
		ch <- prometheus.MustNewConstMetric(
			statIOWrites,
			prometheus.CounterValue,
			writesMetric,
			labels...,
		)

		if writeTime.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOWriteTime,
				prometheus.CounterValue,
				writeTime.Float64,
				labels...,
			)
		}

		writebacksMetric := 0.0
		if writebacks.Valid {
			writebacksMetric = float64(writebacks.Int64)
		}
		if writebacks.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOWritebacks,
				prometheus.CounterValue,
				writebacksMetric,
				labels...,
			)
		}

		if writebackTime.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOWritebackTime,
				prometheus.CounterValue,
				writebackTime.Float64,
				labels...,
			)
		}

		extendsMetric := 0.0
		if extends.Valid {
			extendsMetric = float64(extends.Int64)
		}
		if extends.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOExtends,
				prometheus.CounterValue,
				extendsMetric,
				labels...,
			)
		}

		if extendTime.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOExtendTime,
				prometheus.CounterValue,
				extendTime.Float64,
				labels...,
			)
		}

		hitsMetric := 0.0
		if hits.Valid {
			hitsMetric = float64(hits.Int64)
		}
		if hits.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOHits,
				prometheus.CounterValue,
				hitsMetric,
				labels...,
			)
		}
		evictionsMetric := 0.0
		if evictions.Valid {
			evictionsMetric = float64(evictions.Int64)
		}
		if evictions.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOEvictions,
				prometheus.CounterValue,
				evictionsMetric,
				labels...,
			)
		}
		reuesesMetric := 0.0
		if reueses.Valid {
			reuesesMetric = float64(reueses.Int64)
		}
		if reueses.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOReueses,
				prometheus.CounterValue,
				reuesesMetric,
				labels...,
			)
		}

		fsyncsMetric := 0.0
		if fsyncs.Valid {
			fsyncsMetric = float64(fsyncs.Int64)
		}
		if fsyncs.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOFsyncs,
				prometheus.CounterValue,
				fsyncsMetric,
				labels...,
			)
		}

		if fsyncTime.Valid {
			ch <- prometheus.MustNewConstMetric(
				statIOFsyncTime,
				prometheus.CounterValue,
				fsyncTime.Float64,
				labels...,
			)
		}

		// PostgreSQL 18+ byte statistics
		if v18plus {
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
	}

	return nil
}
