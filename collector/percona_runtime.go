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

	"github.com/percona/postgres_exporter/config"
	"github.com/percona/postgres_exporter/exporter"
	"golang.org/x/sync/semaphore"
)

// NewProbeRuntime returns the runtime of a /probe request. Like the collectors of the metrics
// endpoint, its collectors stop with ctx, share the connection limit sem and query the target only.
func NewProbeRuntime(ctx context.Context, sem *semaphore.Weighted, validatedConfig config.ValidatedConfig, logger *slog.Logger) (*Runtime, error) {
	runtime, err := NewRuntime(validatedConfig, logger)
	if err != nil {
		return nil, err
	}

	cfg := validatedConfig.Config()
	runtime.exporter = exporter.NewExporter(cfg.DataSourceNames, logger, append(exporterOptions(cfg),
		exporter.CollectorName("exporter"),
		exporter.WithContext(ctx),
		exporter.WithConnectionsSemaphore(sem),
	)...)
	if runtime.postgresCollector != nil {
		runtime.postgresCollector.percona = perconaCollector{ctx: ctx, connSema: sem}
		runtime.postgresCollector.databaseDiscovery = nil
	}
	return runtime, nil
}
