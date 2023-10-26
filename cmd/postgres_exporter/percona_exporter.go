package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/blang/semver/v4"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

type MetricResolution string

const (
	DISABLED MetricResolution = ""
	LR       MetricResolution = "lr"
	MR       MetricResolution = "mr"
	HR       MetricResolution = "hr"
)

var (
	collectCustomQueryLr          = kingpin.Flag("collect.custom_query.lr", "Enable custom queries with low resolution directory.").Default("false").Envar("PG_EXPORTER_EXTEND_QUERY_LR").Bool()
	collectCustomQueryMr          = kingpin.Flag("collect.custom_query.mr", "Enable custom queries with medium resolution directory.").Default("false").Envar("PG_EXPORTER_EXTEND_QUERY_MR").Bool()
	collectCustomQueryHr          = kingpin.Flag("collect.custom_query.hr", "Enable custom queries with high resolution directory.").Default("false").Envar("PG_EXPORTER_EXTEND_QUERY_HR").Bool()
	collectCustomQueryLrDirectory = kingpin.Flag("collect.custom_query.lr.directory", "Path to custom queries with low resolution directory.").Envar("PG_EXPORTER_EXTEND_QUERY_LR_PATH").String()
	collectCustomQueryMrDirectory = kingpin.Flag("collect.custom_query.mr.directory", "Path to custom queries with medium resolution directory.").Envar("PG_EXPORTER_EXTEND_QUERY_MR_PATH").String()
	collectCustomQueryHrDirectory = kingpin.Flag("collect.custom_query.hr.directory", "Path to custom queries with high resolution directory.").Envar("PG_EXPORTER_EXTEND_QUERY_HR_PATH").String()
)

func initializePerconaExporters(dsn []string, servers *Servers) (func(), *Exporter, *Exporter, *Exporter) {
	queriesPath := map[MetricResolution]string{
		HR: *collectCustomQueryHrDirectory,
		MR: *collectCustomQueryMrDirectory,
		LR: *collectCustomQueryLrDirectory,
	}

	excludedDatabases := strings.Split(*excludeDatabases, ",")
	opts := []ExporterOpt{
		DisableDefaultMetrics(true),
		DisableSettingsMetrics(true),
		AutoDiscoverDatabases(*autoDiscoverDatabases),
		WithServers(servers),
		WithUserQueriesPath(queriesPath),
		ExcludeDatabases(excludedDatabases),
	}
	hrExporter := NewExporter(dsn,
		append(opts,
			CollectorName("custom_query.hr"),
			WithUserQueriesResolutionEnabled(HR),
			WithEnabled(*collectCustomQueryHr),
			WithConstantLabels(*constantLabelsList),
		)...,
	)
	prometheus.MustRegister(hrExporter)

	mrExporter := NewExporter(dsn,
		append(opts,
			CollectorName("custom_query.mr"),
			WithUserQueriesResolutionEnabled(MR),
			WithEnabled(*collectCustomQueryMr),
			WithConstantLabels(*constantLabelsList),
		)...,
	)
	prometheus.MustRegister(mrExporter)

	lrExporter := NewExporter(dsn,
		append(opts,
			CollectorName("custom_query.lr"),
			WithUserQueriesResolutionEnabled(LR),
			WithEnabled(*collectCustomQueryLr),
			WithConstantLabels(*constantLabelsList),
		)...,
	)
	prometheus.MustRegister(lrExporter)

	return func() {
		hrExporter.servers.Close()
		mrExporter.servers.Close()
		lrExporter.servers.Close()
	}, hrExporter, mrExporter, lrExporter
}

func (e *Exporter) loadCustomQueries(res MetricResolution, version semver.Version, server *Server) {
	if e.userQueriesPath[res] != "" {
		fi, err := os.ReadDir(e.userQueriesPath[res])
		if err != nil {
			level.Error(logger).Log("msg", fmt.Sprintf("failed read dir %q for custom query", e.userQueriesPath[res]),
				"err", err)
			return
		}
		level.Debug(logger).Log("msg", fmt.Sprintf("reading dir %q for custom query", e.userQueriesPath[res]))

		for _, v := range fi {
			if v.IsDir() {
				continue
			}

			if filepath.Ext(v.Name()) == ".yml" || filepath.Ext(v.Name()) == ".yaml" {
				path := filepath.Join(e.userQueriesPath[res], v.Name())
				e.addCustomQueriesFromFile(path, version, server)
			}
		}
	}
}

func (e *Exporter) addCustomQueriesFromFile(path string, version semver.Version, server *Server) {
	// Calculate the hashsum of the useQueries
	userQueriesData, err := os.ReadFile(path)
	if err != nil {
		level.Error(logger).Log("msg", "Failed to reload user queries:"+path, "err", err)
		e.userQueriesError.WithLabelValues(path, "").Set(1)
		return
	}

	hashsumStr := fmt.Sprintf("%x", sha256.Sum256(userQueriesData))

	if err := addQueries(userQueriesData, version, server); err != nil {
		level.Error(logger).Log("msg", "Failed to reload user queries:"+path, "err", err)
		e.userQueriesError.WithLabelValues(path, hashsumStr).Set(1)
		return
	}

	// Mark user queries as successfully loaded
	e.userQueriesError.WithLabelValues(path, hashsumStr).Set(0)
}

// NewDB establishes a new connection using DSN.
func NewDB(dsn string) (*sql.DB, error) {
	fingerprint, err := parseFingerprint(dsn)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	level.Info(logger).Log("msg", "Established new database connection", "fingerprint", fingerprint)
	return db, nil
}
