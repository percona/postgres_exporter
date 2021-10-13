package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/percona/exporter_shared"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"go.uber.org/atomic"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	//exporterPort, _         = GetFreePort()
	exporterPort         = 10000
	exporterMetricsEndpoint = fmt.Sprintf("http://localhost:%d/metrics", exporterPort)
	pgPort                  = 55333 // you should not have PG be running on this port
	dataSources             = []string{
		fmt.Sprintf("postgresql://root:root@localhost:%d/postgres", pgPort),
	}
)

func TestRespectConnectivityErrorsOnMetricsQuerying(t *testing.T) {
	pingWorks := atomic.NewBool(false)

	exporterReady := make(chan interface{})
	go runExporter(
		exporterReady,
		CustomServerPing(func(s *Server) error {
			if pingWorks.Load() {
				return nil
			} else {
				return &ErrorConnectToServer{Msg: "connection failure"}
			}
		}),
		CustomGetServerRetry(&CustomGetServerRetryFactory{}),
	)
	<-exporterReady

	fetchMetrics := func() map[string]string {
		result, err := _fetchMetrics(
			"pg_up",
			regexp.MustCompile(fmt.Sprintf("pg_stat_database_(?P<key>tup_fetched).*postgres.*localhost:%d\"} (?P<value>.*)", pgPort)),
		)
		if err != nil {
			panic(err)
		}
		return result
	}

	// initial fetch
	metrics := fetchMetrics()
	if metrics["pg_up"] != "0" {
		t.Fatalf("Postgres is not running, expected pg_up to be 0")
	}
	if _, ok := metrics["tup_fetched"]; ok {
		t.Fatalf("pg_up was reported as 0, but actual metric is present")
	}

	// simulating case when "db ping" is successful and right after that db is not available
	pingWorks.Store(true)

	metrics = fetchMetrics()
	if metrics["pg_up"] != "0" {
		t.Fatalf("Postgres is not running, expected pg_up to be 0")
	}
	if _, ok := metrics["tup_fetched"]; ok {
		t.Fatalf("pg_up was reported as 0, but actual metric is present")
	}
}

func runExporter(ready chan interface{}, opts ...ExporterOpt) {

	exporter := NewExporter(dataSources, opts...)

	defer func() {
		exporter.servers.Close()
	}()
	prometheus.MustRegister(exporter)

	psCollector := prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{})
	goCollector := prometheus.NewGoCollector()

	version.Branch = Branch
	version.BuildDate = BuildDate
	version.Revision = Revision
	version.Version = VersionShort
	prometheus.MustRegister(version.NewCollector("postgres_exporter"))

	go exporter_shared.RunServer("PostgreSQL", fmt.Sprintf(":%d", exporterPort), "/metrics", newHandler(map[string]prometheus.Collector{
		"exporter":         exporter,
		"standard.process": psCollector,
		"standard.go":      goCollector,
	}))
	for {
		_, err := http.Get(exporterMetricsEndpoint)
		if err == nil {
			ready <- nil
			break
		}
		time.Sleep(100 * time.Microsecond)
	}
}

func _fetchMetrics(metrics ...interface{}) (map[string]string, error) {
	result := make(map[string]string)

	resp, err := http.Get(exporterMetricsEndpoint)
	if err != nil {
		return nil, err
	}

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		next := sc.Text()
		for _, metric := range metrics {
			metricStr, ok := metric.(string)
			if ok {
				starts := strings.HasPrefix(next, metricStr)
				if starts {
					keyLen := len(metricStr) + 1
					value := next[keyLen:]
					result[metricStr] = value
				}

				continue
			}

			metricsRE, ok := metric.(*regexp.Regexp)
			yes := strings.HasPrefix(next, "pg_stat_database")
			if ok && yes {
				matches := metricsRE.FindAllStringSubmatch(next, -1)
				if matches != nil {
					key := matches[0][metricsRE.SubexpIndex("key")]
					value := matches[0][metricsRE.SubexpIndex("value")]
					result[key] = value
				}
			}

		}
	}

	return result, nil
}

// --- utils

type CustomGetServerRetryFactory struct {
}

func (c *CustomGetServerRetryFactory) Create() GetServerRetry {
	return &RetryStrategy{
	}
}

type RetryStrategy struct {
	failed bool
}

func (r *RetryStrategy) CanRetry() (yes bool, lastError error) {
	if r.failed {
		return false, errors.New("<test error>")
	}
	return true, nil
}

func (r *RetryStrategy) Fail(e error) {
	r.failed = true
}

func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
