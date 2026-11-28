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
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webConfig protects the exporter with the password "secret" for the user "pmm".
const webConfig = "basic_auth_users:\n  pmm: " + `$2y$04$8y81bZNNUbsA7/Sr/BCaWOBYT3CzY9hlCQFcI8h8GDKrC8kNZP25a` + "\n"

// startExporter starts the exporter for a database that refuses connections.
func startExporter(t *testing.T, args ...string) string {
	t.Helper()
	return startExporterFor(t, unreachableDSN, args...)
}

// startExporterFor starts the exporter for dsn with args and returns its address.
func startExporterFor(t *testing.T, dsn string, args ...string) string {
	t.Helper()
	return runExporter(t, []string{"DATA_SOURCE_NAME=" + dsn}, args...).addr
}

// buildExporter builds the exporter and returns the path of the binary.
func buildExporter(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "postgres_exporter")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
}

// exporterProcess is an exporter a test started.
type exporterProcess struct {
	addr   string
	cmd    *exec.Cmd
	exited chan error
	logs   *bytes.Buffer
}

// runExporter builds the exporter and starts it with env and args on a free port.
func runExporter(t *testing.T, env []string, args ...string) *exporterProcess {
	t.Helper()

	bin := buildExporter(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	p := &exporterProcess{addr: addr, exited: make(chan error, 1), logs: &bytes.Buffer{}}
	p.cmd = exec.CommandContext(t.Context(), bin, append([]string{"--web.listen-address=" + addr}, args...)...)
	p.cmd.Env = append(os.Environ(), env...)
	p.cmd.Stderr = p.logs
	require.NoError(t, p.cmd.Start())

	go func() { p.exited <- p.cmd.Wait() }()
	t.Cleanup(func() {
		_ = p.cmd.Process.Kill()
		<-p.exited
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-p.exited:
			p.exited <- err
			require.FailNow(t, "the exporter exited", "%v\n%s", err, p.logs.String())
		default:
		}

		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			require.NoError(t, conn.Close())
			return p
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.FailNow(t, "the exporter did not start listening")
	return nil
}

// wait waits for the exporter to exit and returns how it exited.
func (p *exporterProcess) wait(t *testing.T) error {
	t.Helper()

	select {
	case err := <-p.exited:
		p.exited <- err
		return err
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the exporter is still running")
		return nil
	}
}

// stop stops the exporter and returns what it logged.
func (p *exporterProcess) stop(t *testing.T) string {
	t.Helper()

	require.NoError(t, p.cmd.Process.Signal(os.Interrupt))
	_ = p.wait(t)
	return p.logs.String()
}

func get(t *testing.T, url, user, password string) (int, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// TestExporterStartsWithTheFlagsOfPMM runs the exporter the way pmm-agent does, with every flag
// pmm-managed can pass for a PostgreSQL service whose collectors are all disabled.
func TestExporterStartsWithTheFlagsOfPMM(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the exporter")
	}

	dir := t.TempDir()
	webConfigPath := filepath.Join(dir, "web.yml")
	require.NoError(t, os.WriteFile(webConfigPath, []byte(webConfig), 0o600))

	addr := startExporter(t,
		"--auto-discover-databases",
		"--collect.custom_query.hr",
		"--collect.custom_query.hr.directory="+dir,
		"--collect.custom_query.lr",
		"--collect.custom_query.lr.directory="+dir,
		"--collect.custom_query.mr",
		"--collect.custom_query.mr.directory="+dir,
		"--exclude-databases=template0,template1,cloudsqladmin,pmm-managed-dev,azure_maintenance,rdsadmin",
		"--log.level=warn",
		"--max-connections=2",
		"--no-collector.database",
		"--no-collector.database_wraparound",
		"--no-collector.extensions",
		"--no-collector.locks",
		"--no-collector.replication",
		"--no-collector.replication_slot",
		"--no-collector.stat_bgwriter",
		"--no-collector.stat_database",
		"--no-collector.stat_user_tables",
		"--no-collector.statio_user_tables",
		"--no-collector.wal",
		"--web.config="+webConfigPath,
		"--web.telemetry-path=/custom",
	)

	status, _ := get(t, "http://"+addr+"/custom", "", "")
	assert.Equal(t, http.StatusUnauthorized, status)

	status, _ = get(t, "http://"+addr+"/custom", "pmm", "wrong")
	assert.Equal(t, http.StatusUnauthorized, status)

	status, body := get(t, "http://"+addr+"/custom?collect[]=standard.go&collect[]=postgres", "pmm", "secret")
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "go_goroutines")
	// Nothing is left to collect once pmm-managed has disabled every collector it knows.
	assert.NotContains(t, body, "pg_scrape_collector_success")

	status, _ = get(t, "http://"+addr+"/metrics", "pmm", "secret")
	assert.Equal(t, http.StatusNotFound, status)

	status, _ = get(t, "http://"+addr+"/debug/pprof/", "pmm", "secret")
	assert.Equal(t, http.StatusOK, status)
}

func TestParallelScrapesAreNotLimitedByDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the exporter")
	}

	// A database that takes connections and never answers keeps every scrape busy until its scrape timeout.
	database, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	addr := startExporterFor(t, "postgres://user:pass@"+database.Addr().String()+"/postgres?sslmode=disable&connect_timeout=2")

	// More scrapes at once than upstream's --web.max-requests default of 40 lets through.
	statuses := make([]int, 60)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() {
			req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/metrics?collect[]=exporter", nil)
			if !assert.NoError(t, err) {
				return
			}
			req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", "2")
			resp, err := http.DefaultClient.Do(req)
			if !assert.NoError(t, err) {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			statuses[i] = resp.StatusCode
		})
	}
	wg.Wait()

	for _, status := range statuses {
		assert.Equal(t, http.StatusOK, status)
	}
}

func TestProbeSharesTheConnectionLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the exporter")
	}

	// A database that takes connections, never answers and remembers how many were open at once.
	database, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	var open, peak atomic.Int32
	go func() {
		for {
			conn, err := database.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				n := open.Add(1)
				defer open.Add(-1)
				for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
				}
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()

	addr := startExporter(t, "--max-connections=1")
	target := url.QueryEscape(database.Addr().String() + "/postgres?sslmode=disable&connect_timeout=1")

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			resp, err := http.Get("http://" + addr + "/probe?target=" + target)
			if !assert.NoError(t, err) {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
	wg.Wait()

	assert.EqualValues(t, 1, peak.Load())
}

func TestStartupLogsOfTheExporter(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the exporter")
	}

	p := runExporter(t, []string{
		"DATA_SOURCE_NAME=" + unreachableDSN,
		"DATA_SOURCE_PASS_FILE=/nonexistent",
		"PG_EXPORTER_MAX_CONNECTIONS=3",
	})
	logs := p.stop(t)

	assert.Contains(t, logs, `msg="Listening on address" address=[`+p.addr+`]`)
	assert.NotContains(t, logs, "Starting postgres_exporter")
	assert.NotContains(t, logs, "Build context")
	assert.NotContains(t, logs, "DEPRECATED")
}

func TestVersionOfTheExporter(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the exporter")
	}

	out, err := exec.Command(buildExporter(t), "--version").CombinedOutput()
	require.NoError(t, err, string(out))
	assert.True(t, strings.HasPrefix(string(out), "postgres_exporter, version "), string(out))
}

func TestSIGHUPStopsTheExporter(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the exporter")
	}

	p := runExporter(t, []string{"DATA_SOURCE_NAME=" + unreachableDSN})
	require.NoError(t, p.cmd.Process.Signal(syscall.SIGHUP))

	var exitErr *exec.ExitError
	require.ErrorAs(t, p.wait(t), &exitErr)
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	assert.Equal(t, syscall.SIGHUP, status.Signal())
}
