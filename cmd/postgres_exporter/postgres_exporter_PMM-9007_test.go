package main

import (
	"go.uber.org/atomic"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreakerForDB(t *testing.T) {
	it := NewPGIntegrationTest()
	delay := atomic.NewInt64(0) // no delay as scraping is called on the startup
	it.RunExporter(
		DBCircuitBreakerConfig(10),
		CustomServerPing(func(s *Server) error {
			time.Sleep(time.Duration(delay.Load()))
			return &ErrorConnectToServer{Msg: "connection failure"}
		}),
		CustomGetServerRetry(&CustomGetServerRetryFactory{}),
	)
	go func() {
		it.StopExporter()
	}()

	fetchMetrics := func() map[string]string {
		result, err := it.FetchMetrics(
			"pg_up",
		)
		if err != nil {
			panic(err)
		}
		return result
	}

	_ = fetchMetrics() // should trigger circuit breaker

	delay.Store(42 * int64(time.Hour)) // now we have some delay
	timeout := 1 * time.Second
	timesToAdd := 10
	wg := new(sync.WaitGroup)
	wg.Add(timesToAdd)
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		wg.Wait()
	}()

	for i := 0; i < timesToAdd; i++ {
		go func() {
			metrics := fetchMetrics()
			if metrics["pg_up"] != "0" {
				panic("Postgres is not running, expected pg_up to be 0")
			}
			wg.Done()
		}()
	}

	select {
	case <-ready:
		//	test passed
	case <-time.After(timeout):
		t.Fatalf("timeout, it looks like circuit breaker is not functioning")
	}
}
