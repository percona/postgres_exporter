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
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
)

// PerconaPGStatWalReceiverCollector is the stat_wal_receiver collector of PMM. It reports a WAL
// receiver only when its slot names a repmgr upstream node.
type PerconaPGStatWalReceiverCollector struct {
	log *slog.Logger
}

func NewPerconaPGStatWalReceiverCollector(config collectorConfig) (Collector, error) {
	return &PerconaPGStatWalReceiverCollector{log: config.logger}, nil
}

const (
	perconaPGStatWalColumnQuery = `
	SELECT
		column_name
	FROM information_schema.columns
	WHERE
		table_name = 'pg_stat_wal_receiver' and
		column_name = 'flushed_lsn'
	`

	perconaPGStatWalReceiverQueryTemplate = `
	SELECT
		trim(both '''' from substring(conninfo from 'host=([^ ]*)')) as upstream_host,
		slot_name,
		status,
		(receive_start_lsn- '0/0') %% (2^52)::bigint as receive_start_lsn,
		%s
receive_start_tli,
		received_tli,
		extract(epoch from last_msg_send_time) as last_msg_send_time,
		extract(epoch from last_msg_receipt_time) as last_msg_receipt_time,
		(latest_end_lsn - '0/0') %% (2^52)::bigint as latest_end_lsn,
		extract(epoch from latest_end_time) as latest_end_time,
		substring(slot_name from 'repmgr_slot_([0-9]*)') as upstream_node
	FROM pg_catalog.pg_stat_wal_receiver
	`
)

func (c PerconaPGStatWalReceiverCollector) Update(ctx context.Context, instance *instance, ch chan<- prometheus.Metric) error {
	// pg_stat_get_wal_receiver is not supported on Aurora.
	if instance.isAurora {
		return nil
	}
	db := instance.getDB()

	hasFlushedLSNRows, err := db.QueryContext(ctx, perconaPGStatWalColumnQuery)
	if err != nil {
		return err
	}
	hasFlushedLSN := hasFlushedLSNRows.Next()
	query := fmt.Sprintf(perconaPGStatWalReceiverQueryTemplate, "")
	if hasFlushedLSN {
		query = fmt.Sprintf(perconaPGStatWalReceiverQueryTemplate, "(flushed_lsn - '0/0') % (2^52)::bigint as flushed_lsn,\n")
	}
	hasFlushedLSNRows.Close()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var upstreamHost, slotName, status sql.NullString
		var receiveStartLsn, receiveStartTli, flushedLsn, receivedTli, latestEndLsn, upstreamNode sql.NullInt64
		var lastMsgSendTime, lastMsgReceiptTime, latestEndTime sql.NullFloat64

		if hasFlushedLSN {
			err = rows.Scan(&upstreamHost, &slotName, &status, &receiveStartLsn, &receiveStartTli, &flushedLsn, &receivedTli, &lastMsgSendTime, &lastMsgReceiptTime, &latestEndLsn, &latestEndTime, &upstreamNode)
		} else {
			err = rows.Scan(&upstreamHost, &slotName, &status, &receiveStartLsn, &receiveStartTli, &receivedTli, &lastMsgSendTime, &lastMsgReceiptTime, &latestEndLsn, &latestEndTime, &upstreamNode)
		}
		if err != nil {
			return err
		}

		if !upstreamHost.Valid {
			c.log.Debug("Skipping wal receiver stats because upstream host is null")
			continue
		}
		if !slotName.Valid {
			c.log.Debug("Skipping wal receiver stats because slotname host is null")
			continue
		}
		if !status.Valid {
			c.log.Debug("Skipping wal receiver stats because status is null")
			continue
		}
		labels := []string{upstreamHost.String, slotName.String, status.String}

		if !receiveStartLsn.Valid {
			c.log.Debug("Skipping wal receiver stats because receive_start_lsn is null")
			continue
		}
		if !receiveStartTli.Valid {
			c.log.Debug("Skipping wal receiver stats because receive_start_tli is null")
			continue
		}
		if hasFlushedLSN && !flushedLsn.Valid {
			c.log.Debug("Skipping wal receiver stats because flushed_lsn is null")
			continue
		}
		if !receivedTli.Valid {
			c.log.Debug("Skipping wal receiver stats because received_tli is null")
			continue
		}
		if !lastMsgSendTime.Valid {
			c.log.Debug("Skipping wal receiver stats because last_msg_send_time is null")
			continue
		}
		if !lastMsgReceiptTime.Valid {
			c.log.Debug("Skipping wal receiver stats because last_msg_receipt_time is null")
			continue
		}
		if !latestEndLsn.Valid {
			c.log.Debug("Skipping wal receiver stats because latest_end_lsn is null")
			continue
		}
		if !latestEndTime.Valid {
			c.log.Debug("Skipping wal receiver stats because latest_end_time is null")
			continue
		}
		if !upstreamNode.Valid {
			c.log.Debug("Skipping wal receiver stats because upstream_node is null")
			continue
		}

		ch <- prometheus.MustNewConstMetric(statWalReceiverReceiveStartLsn, prometheus.CounterValue, float64(receiveStartLsn.Int64), labels...)
		ch <- prometheus.MustNewConstMetric(statWalReceiverReceiveStartTli, prometheus.GaugeValue, float64(receiveStartTli.Int64), labels...)
		if hasFlushedLSN {
			ch <- prometheus.MustNewConstMetric(statWalReceiverFlushedLSN, prometheus.CounterValue, float64(flushedLsn.Int64), labels...)
		}
		ch <- prometheus.MustNewConstMetric(statWalReceiverReceivedTli, prometheus.GaugeValue, float64(receivedTli.Int64), labels...)
		ch <- prometheus.MustNewConstMetric(statWalReceiverLastMsgSendTime, prometheus.CounterValue, lastMsgSendTime.Float64, labels...)
		ch <- prometheus.MustNewConstMetric(statWalReceiverLastMsgReceiptTime, prometheus.CounterValue, lastMsgReceiptTime.Float64, labels...)
		ch <- prometheus.MustNewConstMetric(statWalReceiverLatestEndLsn, prometheus.CounterValue, float64(latestEndLsn.Int64), labels...)
		ch <- prometheus.MustNewConstMetric(statWalReceiverLatestEndTime, prometheus.CounterValue, latestEndTime.Float64, labels...)
		ch <- prometheus.MustNewConstMetric(statWalReceiverUpstreamNode, prometheus.GaugeValue, float64(upstreamNode.Int64), labels...)
	}
	return rows.Err()
}
