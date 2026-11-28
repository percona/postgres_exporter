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
	"gopkg.in/yaml.v2"

	"github.com/percona/postgres_exporter/distribution"
)

// notSupportedByAurora as the value of query_aurora skips the query on Aurora instances.
const notSupportedByAurora = "!"

// applyDistributionQueries replaces the queries of user defined metrics with the ones written for
// the distribution of the server. On Aurora, query_aurora wins over query when it is set, and the
// metric is dropped when it is set to notSupportedByAurora.
func applyDistributionQueries(content []byte, dist string, metricMaps map[string]intermediateMetricMap, queryOverrides map[string]string) error {
	if dist != distribution.Aurora {
		return nil
	}

	var auroraQueries map[string]struct {
		QueryAurora string `yaml:"query_aurora"`
	}
	err := yaml.Unmarshal(content, &auroraQueries)
	if err != nil {
		return err
	}

	for metric, specs := range auroraQueries {
		switch specs.QueryAurora {
		case "":
		case notSupportedByAurora:
			delete(metricMaps, metric)
			delete(queryOverrides, metric)
		default:
			queryOverrides[metric] = specs.QueryAurora
		}
	}
	return nil
}
