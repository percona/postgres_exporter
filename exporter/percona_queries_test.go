// Copyright (C) 2023 Percona LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package exporter

import (
	"testing"

	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/require"
)

func TestParseUserQueries_DistributionSelection(t *testing.T) {
	cases := []struct {
		name         string
		yamlInput    string
		distribution string
		wantQuery    string
		wantMapped   bool
	}{
		{
			name: "Standard uses query",
			yamlInput: `
pg_replication:
  query: "standard"
  query_aurora: "aurora"
`,
			distribution: "standard",
			wantQuery:    "standard",
			wantMapped:   true,
		},
		{
			name: "Aurora uses query_aurora",
			yamlInput: `
pg_replication:
  query: "standard"
  query_aurora: "aurora"
`,
			distribution: "aurora",
			wantQuery:    "aurora",
			wantMapped:   true,
		},
		{
			name: "Aurora falls back to query",
			yamlInput: `
pg_replication:
  query: "standard"
`,
			distribution: "aurora",
			wantQuery:    "standard",
			wantMapped:   true,
		},
		{
			name: "Aurora skips if neither",
			yamlInput: `
pg_replication:
`,
			distribution: "aurora",
			wantQuery:    "",
			wantMapped:   true,
		},
		{
			name: "Standard query only",
			yamlInput: `
pg_replication:
  query: "standard"
`,
			distribution: "standard",
			wantQuery:    "standard",
			wantMapped:   true,
		},
		{
			name: "Aurora query only",
			yamlInput: `
pg_replication:
  query_aurora: "aurora"
`,
			distribution: "aurora",
			wantQuery:    "aurora",
			wantMapped:   true,
		},
		{
			name: "Not supported by Aurora",
			yamlInput: `
pg_replication:
  query: "standard"
  query_aurora: "!"
`,
			distribution: "aurora",
			wantQuery:    "",
			wantMapped:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metricMaps, metricsQueries, err := parseUserQueries([]byte(tc.yamlInput), promslog.NewNopLogger())
			require.NoError(t, err)
			err = applyDistributionQueries([]byte(tc.yamlInput), tc.distribution, metricMaps, metricsQueries)
			require.NoError(t, err)
			require.Equal(t, tc.wantQuery, metricsQueries["pg_replication"])
			_, mapped := metricMaps["pg_replication"]
			require.Equal(t, tc.wantMapped, mapped)
		})
	}
}
