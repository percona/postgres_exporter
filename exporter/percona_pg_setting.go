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
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

const settingsSubsystem = "settings"

var settingUnits = []string{
	"ms", "s", "min", "h", "d",
	"B", "kB", "MB", "GB", "TB",
}

// pgSettingsQuery skips the settings that the settings collector skips for their invalid format.
const pgSettingsQuery = "SELECT name, setting, COALESCE(unit, ''), short_desc, vartype FROM pg_settings WHERE vartype IN ('bool', 'integer', 'real') AND name NOT IN ('sync_commit_cancel_wait', 'google_dataplex.max_messages');"

// querySettings exposes the pg_settings view with the labels of the server, which the settings collector does not add.
func querySettings(ctx context.Context, ch chan<- prometheus.Metric, server *Server) error {
	server.logger.Debug("Querying pg_setting view", "server", server)

	rows, err := server.db.QueryContext(ctx, pgSettingsQuery)
	if err != nil {
		return fmt.Errorf("error running query on database '%s': %s %w", server, settingsSubsystem, err)
	}
	defer rows.Close() // nolint: errcheck

	for rows.Next() {
		s := &pgSetting{}
		err = rows.Scan(&s.name, &s.setting, &s.unit, &s.shortDesc, &s.vartype)
		if err != nil {
			return fmt.Errorf("error retrieving rows on '%s': %s %w", server, settingsSubsystem, err)
		}

		ch <- s.metric(server.labels)
	}

	return nil
}

// pgSetting represents a PostgreSQL runtime variable as returned by the pg_settings view.
type pgSetting struct {
	name, setting, unit, shortDesc, vartype string
}

// metric panics on a setting it cannot expose, so that no setting goes missing unnoticed.
func (s *pgSetting) metric(labels prometheus.Labels) prometheus.Metric {
	var (
		err       error
		name      = strings.ReplaceAll(s.name, ".", "_")
		unit      = s.unit // nolint: ineffassign
		shortDesc = fmt.Sprintf("Server Parameter: %s", s.name)
		val       float64
	)

	switch s.vartype {
	case "bool":
		if s.setting == "on" {
			val = 1
		}
	case "integer", "real":
		val, unit, err = s.normaliseUnit()
		if err != nil {
			panic(err)
		}

		if len(unit) > 0 {
			name = fmt.Sprintf("%s_%s", name, unit)
			shortDesc = fmt.Sprintf("%s [Units converted to %s.]", shortDesc, unit)
		}
	default:
		panic(fmt.Sprintf("Unsupported vartype %q", s.vartype))
	}

	desc := prometheus.NewDesc(prometheus.BuildFQName(namespace, settingsSubsystem, name), shortDesc, nil, labels)
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, val)
}

// sanitizeValue removes the unit Aurora appends to some setting values.
func (s *pgSetting) sanitizeValue() {
	for _, unit := range settingUnits {
		if strings.HasSuffix(s.setting, unit) {
			endPos := len(s.setting) - len(unit) - 1
			s.setting = s.setting[:endPos]
			return
		}
	}
}

// normaliseUnit converts the setting to seconds or bytes, the base units of its unit family.
func (s *pgSetting) normaliseUnit() (float64, string, error) {
	s.sanitizeValue()

	val, err := strconv.ParseFloat(s.setting, 64)
	if err != nil {
		return val, "", fmt.Errorf("error converting setting '%s' value '%s' to float: %w", s.name, s.setting, err)
	}

	// Units defined in: https://www.postgresql.org/docs/current/static/config-setting.html
	var unit string
	switch s.unit {
	case "":
		return val, "", nil
	case "ms", "s", "min", "h", "d":
		unit = "seconds"
	case "B", "kB", "MB", "GB", "TB", "1kB", "2kB", "4kB", "8kB", "16kB", "32kB", "64kB", "16MB", "32MB", "64MB":
		unit = "bytes"
	default:
		return val, "", fmt.Errorf("unknown unit for runtime variable: '%s'", s.unit)
	}

	// -1 is special, don't modify the value
	if val == -1 {
		return val, unit, nil
	}

	switch s.unit {
	case "ms":
		val /= 1000
	case "min":
		val *= 60
	case "h":
		val *= 60 * 60
	case "d":
		val *= 60 * 60 * 24
	case "kB":
		val *= math.Pow(2, 10)
	case "MB":
		val *= math.Pow(2, 20)
	case "GB":
		val *= math.Pow(2, 30)
	case "TB":
		val *= math.Pow(2, 40)
	case "1kB":
		val *= math.Pow(2, 10)
	case "2kB":
		val *= math.Pow(2, 11)
	case "4kB":
		val *= math.Pow(2, 12)
	case "8kB":
		val *= math.Pow(2, 13)
	case "16kB":
		val *= math.Pow(2, 14)
	case "32kB":
		val *= math.Pow(2, 15)
	case "64kB":
		val *= math.Pow(2, 16)
	case "16MB":
		val *= math.Pow(2, 24)
	case "32MB":
		val *= math.Pow(2, 25)
	case "64MB":
		val *= math.Pow(2, 26)
	}

	return val, unit, nil
}
