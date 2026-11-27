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

package main

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/percona/postgres_exporter/collector"
	"github.com/percona/postgres_exporter/config"
	"github.com/percona/postgres_exporter/exporter"
	"github.com/prometheus/client_golang/prometheus"
	prometheuscollectors "github.com/prometheus/client_golang/prometheus/collectors"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"golang.org/x/sync/semaphore"
)

// Values of the collect[] query parameter that select what a scrape returns.
const (
	collectExporter        = "exporter"
	collectCustomQueryHR   = "custom_query.hr"
	collectCustomQueryMR   = "custom_query.mr"
	collectCustomQueryLR   = "custom_query.lr"
	collectPostgres        = "postgres"
	collectStandardGo      = "standard.go"
	collectStandardProcess = "standard.process"
	collectVersion         = "version"
)

// defaultScrapeTimeout applies when the scraper does not send the X-Prometheus-Scrape-Timeout-Seconds header.
const defaultScrapeTimeout = 10 * time.Second

var (
	collectCustomQueryLr          = kingpin.Flag("collect.custom_query.lr", "Enable custom queries with low resolution directory.").Default("false").Envar("PG_EXPORTER_EXTEND_QUERY_LR").Bool()
	collectCustomQueryMr          = kingpin.Flag("collect.custom_query.mr", "Enable custom queries with medium resolution directory.").Default("false").Envar("PG_EXPORTER_EXTEND_QUERY_MR").Bool()
	collectCustomQueryHr          = kingpin.Flag("collect.custom_query.hr", "Enable custom queries with high resolution directory.").Default("false").Envar("PG_EXPORTER_EXTEND_QUERY_HR").Bool()
	collectCustomQueryLrDirectory = kingpin.Flag("collect.custom_query.lr.directory", "Path to custom queries with low resolution directory.").Envar("PG_EXPORTER_EXTEND_QUERY_LR_PATH").String()
	collectCustomQueryMrDirectory = kingpin.Flag("collect.custom_query.mr.directory", "Path to custom queries with medium resolution directory.").Envar("PG_EXPORTER_EXTEND_QUERY_MR_PATH").String()
	collectCustomQueryHrDirectory = kingpin.Flag("collect.custom_query.hr.directory", "Path to custom queries with high resolution directory.").Envar("PG_EXPORTER_EXTEND_QUERY_HR_PATH").String()

	maxConnections         = kingpin.Flag("max-connections", "Maximum number of connections to use").Default(strconv.FormatInt(config.DefaultMaxConnections, 10)).Envar("PG_EXPORTER_MAX_CONNECTIONS").Int64()
	disableSettingsMetrics = kingpin.Flag("disable-settings-metrics", "Do not include pg_settings metrics.").Default("false").Envar("PG_EXPORTER_DISABLE_SETTINGS_METRICS").Bool()

	// PMM 2 passes the web configuration file with this flag.
	webConfigFile = kingpin.Flag("web.config", "[EXPERIMENTAL] Path to config yaml file that can enable TLS or authentication. Alias of --web.config.file.").Default("").String()
)

func perconaConfig() config.PerconaConfig {
	return config.PerconaConfig{
		DisableSettingsMetrics: *disableSettingsMetrics,
		MaxConnections:         *maxConnections,
		CustomQueriesHR:        config.CustomQueriesConfig{Enabled: *collectCustomQueryHr, Directory: *collectCustomQueryHrDirectory},
		CustomQueriesMR:        config.CustomQueriesConfig{Enabled: *collectCustomQueryMr, Directory: *collectCustomQueryMrDirectory},
		CustomQueriesLR:        config.CustomQueriesConfig{Enabled: *collectCustomQueryLr, Directory: *collectCustomQueryLrDirectory},
	}
}

// applyPerconaFlagDefaults sets the fork's defaults of the flags exporter-toolkit registers.
func applyPerconaFlagDefaults(app *kingpin.Application) {
	// Parallel requests are not limited; --max-connections bounds what scrapes do to the database.
	app.GetFlag("web.max-requests").Default("0")
}

// applyWebConfigAlias makes --web.config work as --web.config.file.
func applyWebConfigAlias(flags *web.FlagConfig) {
	if *webConfigFile != "" {
		*flags.WebConfigFile = *webConfigFile
	}
}

// Filters selects the collectors of a scrape.
type Filters struct {
	EnableAllCollectors     bool
	EnableLRCollector       bool
	EnableMRCollector       bool
	EnableHRCollector       bool
	EnableDefaultCollector  bool
	EnableGoCollector       bool
	EnableVersionCollector  bool
	EnableProcessCollector  bool
	EnablePostgresCollector bool
}

func newFilters(collect []string) Filters {
	var f Filters
	if len(collect) == 0 {
		f.EnableAllCollectors = true
		return f
	}

	for _, filter := range collect {
		switch filter {
		case collectStandardProcess:
			f.EnableProcessCollector = true
		case collectStandardGo:
			f.EnableGoCollector = true
		case collectVersion:
			f.EnableVersionCollector = true
		case collectExporter:
			f.EnableDefaultCollector = true
		case collectCustomQueryHR:
			f.EnableHRCollector = true
		case collectCustomQueryMR:
			f.EnableMRCollector = true
		case collectCustomQueryLR:
			f.EnableLRCollector = true
		case collectPostgres:
			f.EnablePostgresCollector = true
		}
	}
	return f
}

// perconaHandler serves the metrics endpoint the way PMM scrapes it: every request names the
// collectors it wants in collect[], and gets collectors that live for that request only and share
// a limited number of database connections.
type perconaHandler struct {
	logger   *slog.Logger
	cfg      config.Config
	connSema *semaphore.Weighted

	// exporterMetrics describe the exporter process; a nil map leaves them out of every scrape.
	exporterMetrics map[string]prometheus.Collector
	// configMetrics report the state of the configuration file.
	configMetrics prometheus.Gatherer
}

func newPerconaHandler(logger *slog.Logger, validatedConfig config.ValidatedConfig, configMetrics prometheus.Gatherer, disableExporterMetrics bool) (*perconaHandler, error) {
	cfg := validatedConfig.Config()
	h := &perconaHandler{
		logger:        logger,
		cfg:           cfg,
		connSema:      semaphore.NewWeighted(cfg.Percona.MaxConnections),
		configMetrics: configMetrics,
	}
	if !disableExporterMetrics {
		h.exporterMetrics = map[string]prometheus.Collector{
			collectStandardProcess: prometheuscollectors.NewProcessCollector(prometheuscollectors.ProcessCollectorOpts{}),
			collectStandardGo:      prometheuscollectors.NewGoCollector(),
			collectVersion:         versioncollector.NewCollector(exporterName),
		}
	}

	// Collectors are created on every scrape; make sure at startup that they can be.
	if len(cfg.DataSourceNames) > 0 {
		_, err := h.postgresCollector(context.Background())
		if err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *perconaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	timeout := defaultScrapeTimeout
	seconds, err := strconv.Atoi(r.Header.Get("X-Prometheus-Scrape-Timeout-Seconds"))
	if err == nil {
		timeout = time.Duration(seconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	collect := r.URL.Query()["collect[]"]
	h.logger.Debug("Collect query", "filters", collect)
	filters := newFilters(collect)

	gatherers := prometheus.Gatherers{h.registry(ctx, filters)}
	if filters.EnableAllCollectors || filters.EnableVersionCollector {
		gatherers = append(gatherers, h.configMetrics)
	}

	promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		ErrorLog:      slog.NewLogLogger(h.logger.Handler(), slog.LevelError),
	}).ServeHTTP(w, r)
}

// registry returns the collectors a scrape asked for.
func (h *perconaHandler) registry(ctx context.Context, filters Filters) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	cfg := h.cfg

	if filters.EnableAllCollectors || filters.EnableDefaultCollector {
		registry.MustRegister(h.exporter(ctx, collectExporter,
			exporter.DisableDefaultMetrics(cfg.DisableDefaultMetrics),
			exporter.DisableSettingsMetrics(cfg.Percona.DisableSettingsMetrics),
			exporter.IncludeDatabases(strings.Join(cfg.IncludeDatabases, ",")),
			exporter.WithUserQueriesPath(cfg.UserQueriesPath),
		))
	}

	customQueries := []struct {
		name    string
		enabled bool
		cfg     config.CustomQueriesConfig
	}{
		{collectCustomQueryHR, filters.EnableHRCollector, cfg.Percona.CustomQueriesHR},
		{collectCustomQueryMR, filters.EnableMRCollector, cfg.Percona.CustomQueriesMR},
		{collectCustomQueryLR, filters.EnableLRCollector, cfg.Percona.CustomQueriesLR},
	}
	for _, cq := range customQueries {
		if !cq.cfg.Enabled || (!filters.EnableAllCollectors && !cq.enabled) {
			continue
		}
		registry.MustRegister(h.exporter(ctx, cq.name,
			exporter.DisableDefaultMetrics(true),
			exporter.DisableSettingsMetrics(true),
			exporter.WithCustomQueriesDirectory(cq.cfg.Directory),
		))
	}

	exporterMetrics := []struct {
		name    string
		enabled bool
	}{
		{collectStandardGo, filters.EnableGoCollector},
		{collectStandardProcess, filters.EnableProcessCollector},
		{collectVersion, filters.EnableVersionCollector},
	}
	for _, m := range exporterMetrics {
		c, ok := h.exporterMetrics[m.name]
		if ok && (filters.EnableAllCollectors || m.enabled) {
			registry.MustRegister(c)
		}
	}

	if (filters.EnableAllCollectors || filters.EnablePostgresCollector) && len(cfg.DataSourceNames) > 0 {
		pc, err := h.postgresCollector(ctx)
		if err != nil {
			h.logger.Error("Failed to create PostgresCollector", "err", err)
		} else {
			registry.MustRegister(pc)
		}
	}

	return registry
}

// exporter returns an exporter for one scrape. Its own metrics carry name in the collector label,
// which tells apart the exporters that share a registry.
func (h *perconaHandler) exporter(ctx context.Context, name string, opts ...exporter.ExporterOpt) *exporter.Exporter {
	cfg := h.cfg
	base := []exporter.ExporterOpt{
		exporter.AutoDiscoverDatabases(cfg.AutoDiscoverDatabases),
		exporter.ExcludeDatabases(cfg.ExcludeDatabases),
		exporter.WithConstantLabels(cfg.ConstantLabels),
		exporter.WithMetricPrefix(cfg.MetricPrefix),
		exporter.WrapLargeCounters(cfg.WrapLargeCounters),
		exporter.CollectorName(name),
		exporter.WithContext(ctx),
		exporter.WithConnectionsSemaphore(h.connSema),
	}
	return exporter.NewExporter(cfg.DataSourceNames, h.logger, slices.Concat(base, opts)...)
}

// postgresCollector returns the collectors of the collector package for one scrape. They run
// against the first data source only: the custom queries cover the other databases of the server.
func (h *perconaHandler) postgresCollector(ctx context.Context) (*collector.PostgresCollector, error) {
	cfg := h.cfg
	return collector.NewPostgresCollector(
		h.logger,
		cfg.ExcludeDatabases,
		cfg.DataSourceNames[0],
		nil,
		collector.WithCollectorStates(cfg.Collectors),
		collector.WithLongRunningTransactionsConfig(cfg.LongRunningTransactions),
		collector.WithPGStatStatementsConfig(cfg.PGStatStatements),
		collector.WithWrapLargeCounters(cfg.WrapLargeCounters),
		collector.WithContext(ctx),
		collector.WithConnectionsSemaphore(h.connSema),
	)
}
