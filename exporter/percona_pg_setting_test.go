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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgSettingMetric(t *testing.T) {
	labels := prometheus.Labels{serverLabelName: "db.example:5432"}

	tests := []struct {
		setting pgSetting
		name    string
		value   float64
	}{
		{pgSetting{name: "max_connections", setting: "100", vartype: "integer"}, "pg_settings_max_connections", 100},
		{pgSetting{name: "fsync", setting: "on", vartype: "bool"}, "pg_settings_fsync", 1},
		{pgSetting{name: "jit", setting: "off", vartype: "bool"}, "pg_settings_jit", 0},
		{pgSetting{name: "shared_buffers", setting: "16384", unit: "8kB", vartype: "integer"}, "pg_settings_shared_buffers_bytes", 134217728},
		{pgSetting{name: "checkpoint_timeout", setting: "5", unit: "min", vartype: "integer"}, "pg_settings_checkpoint_timeout_seconds", 300},
		{pgSetting{name: "log_min_duration_statement", setting: "-1", unit: "ms", vartype: "integer"}, "pg_settings_log_min_duration_statement_seconds", -1},
		{pgSetting{name: "seq_page_cost", setting: "1.5", vartype: "real"}, "pg_settings_seq_page_cost", 1.5},
		{pgSetting{name: "rds.rds_superuser_reserved_connections", setting: "2", vartype: "integer"}, "pg_settings_rds_rds_superuser_reserved_connections", 2},
		{pgSetting{name: "some-extension.limit", setting: "3", vartype: "integer"}, "pg_settings_some_extension_limit", 3},
		// Aurora reports some values together with their unit.
		{pgSetting{name: "aurora_setting", setting: "5 s", unit: "s", vartype: "integer"}, "pg_settings_aurora_setting_seconds", 5},
	}
	for _, tt := range tests {
		metric, err := tt.setting.metric(labels)
		require.NoError(t, err, tt.setting.name)

		assert.Contains(t, metric.Desc().String(), `fqName: "`+tt.name+`"`)
		pb := &dto.Metric{}
		require.NoError(t, metric.Write(pb))
		assert.InDelta(t, tt.value, pb.GetGauge().GetValue(), 0, tt.setting.name)
		require.Len(t, pb.Label, 1)
		assert.Equal(t, "db.example:5432", pb.Label[0].GetValue())
	}
}

func TestPgSettingMetricReportsWhatItCannotExpose(t *testing.T) {
	labels := prometheus.Labels{serverLabelName: "db.example:5432"}

	for _, setting := range []pgSetting{
		{name: "unknown_unit", setting: "10", unit: "nonexistent", vartype: "integer"},
		{name: "not_a_number", setting: "many", vartype: "integer"},
		{name: "text_setting", setting: "value", vartype: "string"},
	} {
		metric, err := setting.metric(labels)
		assert.Error(t, err, setting.name)
		assert.Nil(t, metric, setting.name)
	}
}
