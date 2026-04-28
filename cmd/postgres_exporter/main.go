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
	"context"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/percona/postgres_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	vc "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"golang.org/x/sync/semaphore"
)

var (
	c = config.Handler{
		Config: &config.Config{},
	}

	// Default is empty so you don't need a config file if using only DSN settings.
	configFile    = kingpin.Flag("config.file", "Postgres exporter configuration file.").Default("").String()
	webConfig     = kingpinflag.AddFlags(kingpin.CommandLine, ":9187")
	webConfigFile = kingpin.Flag(
		"web.config",
		"[EXPERIMENTAL] Path to config yaml file that can enable TLS or authentication.",
	).Default("").String() // added for compatibility reasons to not break it in PMM 2.
	metricsPath            = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").Envar("PG_EXPORTER_WEB_TELEMETRY_PATH").String()
	disableDefaultMetrics  = kingpin.Flag("disable-default-metrics", "Do not include default metrics.").Default("false").Envar("PG_EXPORTER_DISABLE_DEFAULT_METRICS").Bool()
	disableSettingsMetrics = kingpin.Flag("disable-settings-metrics", "Do not include pg_settings metrics.").Default("false").Envar("PG_EXPORTER_DISABLE_SETTINGS_METRICS").Bool()
	autoDiscoverDatabases  = kingpin.Flag("auto-discover-databases", "Whether to discover the databases on a server dynamically. (DEPRECATED)").Default("false").Envar("PG_EXPORTER_AUTO_DISCOVER_DATABASES").Bool()
	// queriesPath            = kingpin.Flag("extend.query-path", "Path to custom queries to run. (DEPRECATED)").Default("").Envar("PG_EXPORTER_EXTEND_QUERY_PATH").String()
	onlyDumpMaps       = kingpin.Flag("dumpmaps", "Do not run, simply dump the maps.").Bool()
	constantLabelsList = kingpin.Flag("constantLabels", "A list of label=value separated by comma(,). (DEPRECATED)").Default("").Envar("PG_EXPORTER_CONSTANT_LABELS").String()
	excludeDatabases   = kingpin.Flag("exclude-databases", "A list of databases to remove when autoDiscoverDatabases is enabled (DEPRECATED)").Default("").Envar("PG_EXPORTER_EXCLUDE_DATABASES").String()
	includeDatabases   = kingpin.Flag("include-databases", "A list of databases to include when autoDiscoverDatabases is enabled (DEPRECATED)").Default("").Envar("PG_EXPORTER_INCLUDE_DATABASES").String()
	metricPrefix       = kingpin.Flag("metric-prefix", "A metric prefix can be used to have non-default (not \"pg\") prefixes for each of the metrics").Default("pg").Envar("PG_EXPORTER_METRIC_PREFIX").String()
	logger             = log.NewNopLogger()
)

// Metric name parts.
const (
	// Namespace for all metrics.
	namespace = "pg"
	// Subsystems.
	exporter = "exporter"
	// The name of the exporter.
	exporterName = "postgres_exporter"
	// Metric label used for static string data thats handy to send to Prometheus
	// e.g. version
	staticLabelName = "static"
	// Metric label used for server identification.
	serverLabelName = "server"
)

// slogGoKitBridge adapts a *slog.Logger to the go-kit log.Logger interface so
// the rest of the codebase can keep using go-kit log/level patterns.
type slogGoKitBridge struct {
	l *slog.Logger
}

func (b *slogGoKitBridge) Log(keyvals ...interface{}) error {
	if len(keyvals) == 0 {
		return nil
	}
	lvl := slog.LevelInfo
	msg := ""
	var args []any
	for i := 0; i+1 < len(keyvals); i += 2 {
		k := fmt.Sprint(keyvals[i])
		v := keyvals[i+1]
		switch k {
		case "level":
			switch fmt.Sprint(v) {
			case "debug":
				lvl = slog.LevelDebug
			case "warn":
				lvl = slog.LevelWarn
			case "error":
				lvl = slog.LevelError
			}
		case "msg":
			msg = fmt.Sprint(v)
		default:
			args = append(args, k, v)
		}
	}
	b.l.Log(context.Background(), lvl, msg, args...)
	return nil
}

func main() {
	kingpin.Version(version.Print(exporterName))
	promlogConfig := &promslog.Config{}
	promslogflag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	webConfig.WebConfigFile = webConfigFile
	kingpin.Parse()
	logger = &slogGoKitBridge{promslog.New(promlogConfig)}

	if *onlyDumpMaps {
		dumpMaps()
		return
	}

	// Load the config file if specified, otherwise use default/DSN settings
	if *configFile != "" {
		if err := c.ReloadConfig(*configFile, logger); err != nil {
			level.Warn(logger).Log("msg", "Error loading config", "err", err)
		}
	} else {
		level.Debug(logger).Log("msg", "No config file provided, using default/DSN settings")
	}

	dsns, err := getDataSources()
	if err != nil {
		level.Error(logger).Log("msg", "Failed reading data sources", "err", err.Error())
		os.Exit(1)
	}

	excludedDatabases := strings.Split(*excludeDatabases, ",")
	logger.Log("msg", "Excluded databases", "databases", fmt.Sprintf("%v", excludedDatabases))

	if *autoDiscoverDatabases || *excludeDatabases != "" || *includeDatabases != "" {
		level.Warn(logger).Log("msg", "Scraping additional databases via auto discovery is DEPRECATED")
	}

	if *constantLabelsList != "" {
		level.Warn(logger).Log("msg", "Constant labels on all metrics is DEPRECATED")
	}

	versionCollector := vc.NewCollector(exporterName)
	psCollector := collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})
	goCollector := collectors.NewGoCollector()

	globalCollectors := map[string]prometheus.Collector{
		"standard.process": psCollector,
		"standard.go":      goCollector,
		"version":          versionCollector,
	}

	connSema := semaphore.NewWeighted(*maxConnections)
	http.Handle(*metricsPath, Handler(logger, dsns, connSema, globalCollectors))

	if *metricsPath != "/" && *metricsPath != "" {
		landingConfig := web.LandingConfig{
			Name:        "Postgres Exporter",
			Description: "Prometheus PostgreSQL server Exporter",
			Version:     version.Info(),
			Links: []web.LandingLinks{
				{
					Address: *metricsPath,
					Text:    "Metrics",
				},
			},
		}
		landingPage, err := web.NewLandingPage(landingConfig)
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
		http.Handle("/", landingPage)
	}

	http.HandleFunc("/probe", handleProbe(logger, excludedDatabases, connSema))

	level.Info(logger).Log("msg", "Listening on address", "address", *webConfig.WebListenAddresses)
	srv := &http.Server{}
	if err := web.ListenAndServe(srv, webConfig, promslog.New(&promslog.Config{})); err != nil {
		level.Error(logger).Log("msg", "Error running HTTP server", "err", err)
		os.Exit(1)
	}
}
