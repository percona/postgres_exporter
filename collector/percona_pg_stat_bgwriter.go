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

	"github.com/blang/semver/v4"
	"github.com/prometheus/client_golang/prometheus"
)

// perconaBGWriterLabels are the labels the exporter's built-in pg_stat_bgwriter map used to add.
var perconaBGWriterLabels = []string{"collector", "server"}

// PerconaPGStatBGWriterCollector exposes pg_stat_bgwriter the way PMM dashboards expect it on every
// supported PostgreSQL version: each metric with and without the _total suffix, the collector and
// server labels, and the checkpoint columns that PostgreSQL 17 moved to pg_stat_checkpointer.
type PerconaPGStatBGWriterCollector struct{}

func NewPerconaPGStatBGWriterCollector(collectorConfig) (Collector, error) {
	return &PerconaPGStatBGWriterCollector{}, nil
}

type perconaBGWriterMetric struct {
	total *prometheus.Desc
	plain *prometheus.Desc
}

func newPerconaBGWriterMetric(name, help string) perconaBGWriterMetric {
	return perconaBGWriterMetric{
		total: prometheus.NewDesc(prometheus.BuildFQName(namespace, bgWriterSubsystem, name+"_total"), help, perconaBGWriterLabels, prometheus.Labels{}),
		plain: prometheus.NewDesc(prometheus.BuildFQName(namespace, bgWriterSubsystem, name), help, perconaBGWriterLabels, prometheus.Labels{}),
	}
}

var (
	perconaBGWriterCheckpointsTimed = newPerconaBGWriterMetric("checkpoints_timed",
		"Number of scheduled checkpoints that have been performed")
	perconaBGWriterCheckpointsReq = newPerconaBGWriterMetric("checkpoints_req",
		"Number of requested checkpoints that have been performed")
	perconaBGWriterCheckpointsDone = newPerconaBGWriterMetric("checkpoints_done",
		"Number of completed checkpoints")
	perconaBGWriterCheckpointWriteTime = newPerconaBGWriterMetric("checkpoint_write_time",
		"Total amount of time that has been spent in the portion of checkpoint processing where files are written to disk, in milliseconds")
	perconaBGWriterCheckpointSyncTime = newPerconaBGWriterMetric("checkpoint_sync_time",
		"Total amount of time that has been spent in the portion of checkpoint processing where files are synchronized to disk, in milliseconds")
	perconaBGWriterBuffersCheckpoint = newPerconaBGWriterMetric("buffers_checkpoint",
		"Number of buffers written during checkpoints")
	perconaBGWriterBuffersClean = newPerconaBGWriterMetric("buffers_clean",
		"Number of buffers written by the background writer")
	perconaBGWriterMaxwrittenClean = newPerconaBGWriterMetric("maxwritten_clean",
		"Number of times the background writer stopped a cleaning scan because it had written too many buffers")
	perconaBGWriterBuffersBackend = newPerconaBGWriterMetric("buffers_backend",
		"Number of buffers written directly by a backend")
	perconaBGWriterBuffersBackendFsync = newPerconaBGWriterMetric("buffers_backend_fsync",
		"Number of times a backend had to execute its own fsync call (normally the background writer handles those even when the backend does its own write)")
	perconaBGWriterBuffersAlloc = newPerconaBGWriterMetric("buffers_alloc",
		"Number of buffers allocated")
	perconaBGWriterSlruWritten = newPerconaBGWriterMetric("slru_written",
		"Number of SLRU buffers written during checkpoints and restartpoints")
	perconaBGWriterStatsReset = newPerconaBGWriterMetric("stats_reset",
		"Time at which these statistics were last reset")
)

const perconaCheckpointerQueryPrePG18 = `
		SELECT
			num_timed,
			num_requested,
			NULL::bigint as num_done,
			restartpoints_timed,
			restartpoints_req,
			restartpoints_done,
			write_time,
			sync_time,
			buffers_written,
			NULL::bigint as slru_written,
			stats_reset
		FROM pg_stat_checkpointer;`

const perconaCheckpointerQueryPostPG18 = `
		SELECT
			num_timed,
			num_requested,
			num_done,
			restartpoints_timed,
			restartpoints_req,
			restartpoints_done,
			write_time,
			sync_time,
			buffers_written,
			slru_written,
			stats_reset
		FROM pg_stat_checkpointer;`

func (PerconaPGStatBGWriterCollector) Update(ctx context.Context, instance *instance, ch chan<- prometheus.Metric) error {
	db := instance.getDB()

	var cpt, cpr, cpd, bcp, bc, mwc, bb, bbf, ba, slruw sql.NullInt64
	var cpwt, cpst sql.NullFloat64
	var sr sql.NullTime

	v18plus := instance.version.GTE(semver.Version{Major: 18})

	if instance.version.GE(semver.MustParse("17.0.0")) {
		err := db.QueryRowContext(ctx, statBGWriterQueryAfter17).Scan(&bc, &mwc, &ba, &sr)
		if err != nil {
			return err
		}

		checkpointerQuery := perconaCheckpointerQueryPrePG18
		if v18plus {
			checkpointerQuery = perconaCheckpointerQueryPostPG18
		}

		var rpt, rpr, rpd sql.NullInt64
		var csr sql.NullTime

		err = db.QueryRowContext(ctx, checkpointerQuery).Scan(&cpt, &cpr, &cpd, &rpt, &rpr, &rpd, &cpwt, &cpst, &bcp, &slruw, &csr)
		if err != nil {
			return err
		}
	} else {
		err := db.QueryRowContext(ctx, statBGWriterQueryBefore17).Scan(&cpt, &cpr, &cpwt, &cpst, &bcp, &bc, &mwc, &bb, &bbf, &ba, &sr)
		if err != nil {
			return err
		}
	}

	srMetric := 0.0
	if sr.Valid {
		srMetric = float64(sr.Time.Unix())
	}

	wrap := instance.wrapLargeCounters
	values := []struct {
		metric perconaBGWriterMetric
		value  float64
		emit   bool
	}{
		{perconaBGWriterCheckpointsTimed, int64CounterValue(cpt, wrap), true},
		{perconaBGWriterCheckpointsReq, int64CounterValue(cpr, wrap), true},
		{perconaBGWriterCheckpointsDone, int64CounterValue(cpd, wrap), v18plus},
		{perconaBGWriterCheckpointWriteTime, cpwt.Float64, true},
		{perconaBGWriterCheckpointSyncTime, cpst.Float64, true},
		{perconaBGWriterBuffersCheckpoint, int64CounterValue(bcp, wrap), true},
		{perconaBGWriterBuffersClean, int64CounterValue(bc, wrap), true},
		{perconaBGWriterMaxwrittenClean, int64CounterValue(mwc, wrap), true},
		{perconaBGWriterBuffersBackend, int64CounterValue(bb, wrap), true},
		{perconaBGWriterBuffersBackendFsync, int64CounterValue(bbf, wrap), true},
		{perconaBGWriterBuffersAlloc, int64CounterValue(ba, wrap), true},
		{perconaBGWriterSlruWritten, int64CounterValue(slruw, wrap), v18plus},
		{perconaBGWriterStatsReset, srMetric, true},
	}

	labels := []string{"exporter", instance.serverName()}
	for _, v := range values {
		if !v.emit {
			continue
		}
		ch <- prometheus.MustNewConstMetric(v.metric.total, prometheus.CounterValue, v.value, labels...)
	}
	for _, v := range values {
		if !v.emit {
			continue
		}
		ch <- prometheus.MustNewConstMetric(v.metric.plain, prometheus.CounterValue, v.value, labels...)
	}

	return nil
}
