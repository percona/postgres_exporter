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
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
)

// PerconaPGLongRunningTransactionsCollector is the long_running_transactions collector of PMM: it
// counts every transaction that is not idle and reports the time of the scrape as its age.
type PerconaPGLongRunningTransactionsCollector struct {
	log *slog.Logger
}

func NewPerconaPGLongRunningTransactionsCollector(config collectorConfig) (Collector, error) {
	return &PerconaPGLongRunningTransactionsCollector{log: config.logger}, nil
}

const perconaLongRunningTransactionsQuery = `
	SELECT
		COUNT(*) as transactions,
   		MAX(EXTRACT(EPOCH FROM clock_timestamp())) AS oldest_timestamp_seconds
    FROM pg_catalog.pg_stat_activity
    WHERE state is distinct from 'idle' AND query not like 'autovacuum:%'
	`

func (PerconaPGLongRunningTransactionsCollector) Update(ctx context.Context, instance *instance, ch chan<- prometheus.Metric) error {
	db := instance.getDB()
	rows, err := db.QueryContext(ctx, perconaLongRunningTransactionsQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var transactions, ageInSeconds float64

		err := rows.Scan(&transactions, &ageInSeconds)
		if err != nil {
			return err
		}

		ch <- prometheus.MustNewConstMetric(longRunningTransactionsCount, prometheus.GaugeValue, transactions)
		ch <- prometheus.MustNewConstMetric(longRunningTransactionsAgeInSeconds, prometheus.GaugeValue, ageInSeconds)
	}
	return rows.Err()
}
