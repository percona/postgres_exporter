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
	"time"
)

type PGIntegrationTest struct {
	exporter                *Exporter
	exporterPort            int
	pingWorks               *atomic.Bool
	exporterMetricsEndpoint string
}

func NewPGIntegrationTest() *PGIntegrationTest {
	freePort := getFreePort()
	return &PGIntegrationTest{
		pingWorks:               atomic.NewBool(true),
		exporterPort:            freePort,
		exporterMetricsEndpoint: fmt.Sprintf("http://localhost:%d/metrics", freePort),
	}
}

func (it *PGIntegrationTest) RunExporter(opts ...ExporterOpt) {
	exporterReady := make(chan interface{})
	go it.runExporter(
		exporterReady,
		opts...,
	)
	<-exporterReady
}

func (it *PGIntegrationTest) StopExporter() {
	prometheus.Unregister(version.NewCollector("postgres_exporter"))
	prometheus.Unregister(it.exporter)
	it.exporter.servers.Close()
}

func (it *PGIntegrationTest) runExporter(ready chan interface{}, opts ...ExporterOpt) {
	it.exporter = NewExporter(dataSources, opts...)

	prometheus.MustRegister(it.exporter)

	psCollector := prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{})
	goCollector := prometheus.NewGoCollector()

	version.Branch = Branch
	version.BuildDate = BuildDate
	version.Revision = Revision
	version.Version = VersionShort
	prometheus.MustRegister(version.NewCollector("postgres_exporter"))

	go exporter_shared.RunServer("PostgreSQL", fmt.Sprintf(":%d", it.exporterPort), "/metrics", newHandler(map[string]prometheus.Collector{
		"exporter":         it.exporter,
		"standard.process": psCollector,
		"standard.go":      goCollector,
	}))
	for {
		_, err := http.Get(it.exporterMetricsEndpoint)
		if err == nil {
			ready <- nil
			break
		}
		time.Sleep(100 * time.Microsecond)
	}
}

func (it *PGIntegrationTest) FetchMetrics(metrics ...interface{}) (map[string]string, error) {
	result := make(map[string]string)

	resp, err := http.Get(it.exporterMetricsEndpoint)
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

func (it *PGIntegrationTest) SetPingWorks(v bool) {
	it.pingWorks.Store(v)
}

func getFreePort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type CustomGetServerRetryFactory struct {
}

func (c *CustomGetServerRetryFactory) Create() GetServerRetry {
	return &RetryStrategy{}
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
