// Copyright 2021 The Prometheus Authors
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

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUserQueries_DistributionSelection(t *testing.T) {
	cases := []struct {
		name         string
		yamlInput    string
		distribution string
		wantQuery    string
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
		},
		{
			name: "Aurora falls back to query",
			yamlInput: `
pg_replication:
  query: "standard"
`,
			distribution: "aurora",
			wantQuery:    "standard",
		},
		{
			name: "Aurora skips if neither",
			yamlInput: `
pg_replication:
`,
			distribution: "aurora",
			wantQuery:    "",
		},
		{
			name: "Standard query only",
			yamlInput: `
pg_replication:
  query: "standard"
`,
			distribution: "standard",
			wantQuery:    "standard",
		},
		{
			name: "Aurora query only",
			yamlInput: `
pg_replication:
  query_aurora: "aurora"
`,
			distribution: "aurora",
			wantQuery:    "aurora",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, metricsQueries, err := parseUserQueries([]byte(tc.yamlInput), tc.distribution)
			require.NoError(t, err)
			require.Equal(t, tc.wantQuery, metricsQueries["pg_replication"])
		})
	}
}
