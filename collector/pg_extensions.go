package collector

import (
	"context"
	"database/sql"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"strconv"
)

func init() {
	registerCollector("extensions", defaultEnabled, NewExtensionsCollector)
}

var pgExtensions = map[string]*prometheus.Desc{
	"pg_available_extensions": prometheus.NewDesc(
		"pg_available_extensions",
		"Extensions that are available for installation",
		[]string{
			"name",
			"default_version",
			"installed_version",
			"comment",
		},
		prometheus.Labels{},
	),
	"pg_extensions": prometheus.NewDesc(
		"pg_extensions",
		"Installed extensions",
		[]string{
			"extname",
			"extrelocatable",
			"extversion",
		},
		prometheus.Labels{},
	),
}

type ExtensionsCollector struct {
	logger log.Logger
}

func NewExtensionsCollector(logger log.Logger) (Collector, error) {
	return &ExtensionsCollector{logger: logger}, nil
}

func (e *ExtensionsCollector) Update(ctx context.Context, server *server, ch chan<- prometheus.Metric) error {
	db, err := server.GetDB()
	if err != nil {
		return err
	}

	err = e.scrapeAvailableExtensions(ctx, db, ch)
	if err != nil {
		return err
	}

	err = e.scrapeInstalledExtensions(ctx, db, ch)
	if err != nil {
		return err
	}

	return nil
}

func (e *ExtensionsCollector) scrapeInstalledExtensions(ctx context.Context, db *sql.DB, ch chan<- prometheus.Metric) error {
	rowsExtensions, err := db.QueryContext(ctx, `SELECT extname, extrelocatable, extversion FROM pg_extension`)

	if err != nil {
		return err
	}
	defer rowsExtensions.Close()

	for rowsExtensions.Next() {
		var extname string
		var extrelocatable bool
		var extversion string
		if err := rowsExtensions.Scan(&extname, &extrelocatable, &extversion); err != nil {
			return err
		}

		ch <- prometheus.MustNewConstMetric(
			pgExtensions["pg_extensions"],
			prometheus.GaugeValue, 1,
			extname, strconv.FormatBool(extrelocatable), extversion,
		)
	}
	return nil
}

func (e *ExtensionsCollector) scrapeAvailableExtensions(ctx context.Context, db *sql.DB, ch chan<- prometheus.Metric) error {
	rows, err := db.QueryContext(ctx, `SELECT name, default_version, installed_version, comment FROM pg_available_extensions`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var name sql.NullString
		var defaultVersion sql.NullString
		var installedVersion sql.NullString
		var comment sql.NullString

		if err := rows.Scan(&name, &defaultVersion, &installedVersion, &comment); err != nil {
			return err
		}

		ch <- prometheus.MustNewConstMetric(
			pgExtensions["pg_available_extensions"],
			prometheus.GaugeValue, 1,
			name.String, defaultVersion.String, installedVersion.String, comment.String,
		)
	}
	return nil
}
