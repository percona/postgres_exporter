package main

import (
	"fmt"
	"regexp"
	"testing"
)

var (
	//exporterPort         = 10000
	pgPort      = 55333 // you should not have PG be running on this port
	dataSources = []string{
		fmt.Sprintf("postgresql://root:root@localhost:%d/postgres", pgPort),
	}
)

func TestRespectConnectivityErrorsOnMetricsQuerying(t *testing.T) {
	it := NewPGIntegrationTest()
	it.RunExporter(
		CustomServerPing(func(s *Server) error {
			if it.pingWorks.Load() {
				return nil
			} else {
				return &ErrorConnectToServer{Msg: "connection failure"}
			}
		}),
		CustomGetServerRetry(&CustomGetServerRetryFactory{}))
	go func() {
		it.StopExporter()
	}()
	it.SetPingWorks(false)

	fetchMetrics := func() map[string]string {
		result, err := it.FetchMetrics(
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
	it.SetPingWorks(true)

	metrics = fetchMetrics()
	if metrics["pg_up"] != "0" {
		t.Fatalf("Postgres is not running, expected pg_up to be 0")
	}
	if _, ok := metrics["tup_fetched"]; ok {
		t.Fatalf("pg_up was reported as 0, but actual metric is present")
	}
}
