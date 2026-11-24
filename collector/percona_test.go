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
	"regexp"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

var (
	pg16 = semver.MustParse("16.0.0")
	pg17 = semver.MustParse("17.0.0")
	pg18 = semver.MustParse("18.0.0")
)

// testDSN gives test instances the server name testServer.
const (
	testDSN    = "postgres://user:pass@db.example:6432/postgres?sslmode=disable"
	testServer = "db.example:6432"
)

var descNameRe = regexp.MustCompile(`fqName: "([^"]+)"`)

// collected runs update and returns the metrics it sent, keyed by metric name.
func collected(t *testing.T, update func(ch chan<- prometheus.Metric) error) map[string][]MetricResult {
	t.Helper()

	ch := make(chan prometheus.Metric)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		errCh <- update(ch)
	}()

	metrics := make(map[string][]MetricResult)
	for m := range ch {
		name := descNameRe.FindStringSubmatch(m.Desc().String())
		require.Len(t, name, 2, "metric without a name: %s", m.Desc())
		metrics[name[1]] = append(metrics[name[1]], readMetric(m))
	}
	require.NoError(t, <-errCh)
	return metrics
}
