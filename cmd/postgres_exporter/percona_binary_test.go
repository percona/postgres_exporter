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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webConfig protects the exporter with the password "secret" for the user "pmm".
const webConfig = "basic_auth_users:\n  pmm: " + `$2y$04$8y81bZNNUbsA7/Sr/BCaWOBYT3CzY9hlCQFcI8h8GDKrC8kNZP25a` + "\n"

// startExporter builds the exporter, starts it with args on a free port and returns its address.
func startExporter(t *testing.T, args ...string) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "postgres_exporter")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, string(out))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	var logs bytes.Buffer
	cmd := exec.CommandContext(t.Context(), bin, append([]string{"--web.listen-address=" + addr}, args...)...)
	cmd.Env = append(os.Environ(), "DATA_SOURCE_NAME="+unreachableDSN)
	cmd.Stderr = &logs
	require.NoError(t, cmd.Start())

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			exited <- err
			require.FailNow(t, "the exporter exited", "%v\n%s", err, logs.String())
		default:
		}

		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			require.NoError(t, conn.Close())
			return addr
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.FailNow(t, "the exporter did not start listening")
	return ""
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
