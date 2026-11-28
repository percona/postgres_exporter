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
	"database/sql"

	"github.com/blang/semver/v4"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	statDatabaseParallelWorkersToLaunch = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statDatabaseSubsystem, "parallel_workers_to_launch"),
		"Number of parallel workers to launch (PostgreSQL 18+)",
		[]string{"datid", "datname"},
		prometheus.Labels{},
	)
	statDatabaseParallelWorkersLaunched = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, statDatabaseSubsystem, "parallel_workers_launched"),
		"Number of parallel workers launched (PostgreSQL 18+)",
		[]string{"datid", "datname"},
		prometheus.Labels{},
	)
)

// statDatabaseParallelWorkers reads the parallel worker columns pg_stat_database has since PostgreSQL 18.
type statDatabaseParallelWorkers struct {
	available          bool
	toLaunch, launched sql.NullInt64
}

func newStatDatabaseParallelWorkers(version semver.Version) *statDatabaseParallelWorkers {
	return &statDatabaseParallelWorkers{available: version.GTE(semver.Version{Major: 18})}
}

func (p *statDatabaseParallelWorkers) columns(columns []string) []string {
	if !p.available {
		return columns
	}
	return append(columns, "parallel_workers_to_launch", "parallel_workers_launched")
}

func (p *statDatabaseParallelWorkers) scanTargets(targets []any) []any {
	if !p.available {
		return targets
	}
	return append(targets, &p.toLaunch, &p.launched)
}

// valid reports whether the row can be exposed.
func (p *statDatabaseParallelWorkers) valid() bool {
	return !p.available || (p.toLaunch.Valid && p.launched.Valid)
}

func (p *statDatabaseParallelWorkers) emit(ch chan<- prometheus.Metric, wrap bool, labels []string) {
	if !p.available {
		return
	}
	ch <- prometheus.MustNewConstMetric(statDatabaseParallelWorkersToLaunch, prometheus.CounterValue, int64CounterValue(p.toLaunch, wrap), labels...)
	ch <- prometheus.MustNewConstMetric(statDatabaseParallelWorkersLaunched, prometheus.CounterValue, int64CounterValue(p.launched, wrap), labels...)
}
