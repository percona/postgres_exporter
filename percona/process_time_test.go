package upstream_update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/stretchr/testify/assert"
	"github.com/tklauser/go-sysconf"
	"golang.org/x/sys/unix"
)

const (
	exporterWaitTimeoutMs = 1000 // time to wait for exporter process start

	portRangeStart = 20000 // exporter web interface listening port
	portRangeEnd   = 20100 // exporter web interface listening port

	count = 3
	size  = 10
)

type StatsData struct {
	meanMs     float64
	stdDevMs   float64
	stdDevPerc float64
}

func TestCpuTime(t *testing.T) {
	// put postgres_exporter and postgres_exporter_percona files near the test

	t.Run("upstream exporter", func(t *testing.T) {
		latestStats := doTestStats(t, count, size, "../percona/postgres_exporter")
		assert.NotNil(t, latestStats)
	})

	t.Run("percona exporter", func(t *testing.T) {
		latestStats := doTestStats(t, count, size, "../percona/postgres_exporter_percona")
		assert.NotNil(t, latestStats)
	})
}

func preparePerconaExporter(t *testing.T) {
	const downloadLink = "https://github.com/percona/node_exporter/releases/download/v0.17.1/node_exporter_linux_amd64.tar.gz"

	file, err := ioutil.TempFile("/tmp", "node-exporter")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(file.Name())

	out, err := os.Create(file.Name())
	if !assert.NoError(t, err) {
		return
	}

	defer out.Close()

	resp, err := http.Get(downloadLink)
	if !assert.NoError(t, err) {
		return
	}

	defer resp.Body.Close()

	n, err := io.Copy(out, resp.Body)
	if !assert.NoError(t, err) || !assert.NotZero(t, n) {
		return
	}

	cmd := exec.Command("tar", "-xvf", file.Name(), "-C", "/tmp")
	_, err = cmd.Output()

	if err != nil {
		panic(err)
	}
}

func ExtractTarGz(gzipStream io.Reader) {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		log.Fatal("ExtractTarGz: NewReader failed")
	}

	tarReader := tar.NewReader(uncompressedStream)

	for true {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatalf("ExtractTarGz: Next() failed: %s", err.Error())
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(header.Name, 0755); err != nil {
				log.Fatalf("ExtractTarGz: Mkdir() failed: %s", err.Error())
			}
		case tar.TypeReg:
			outFile, err := os.Create(header.Name)
			if err != nil {
				log.Fatalf("ExtractTarGz: Create() failed: %s", err.Error())
			}
			defer outFile.Close()
			if _, err := io.Copy(outFile, tarReader); err != nil {
				log.Fatalf("ExtractTarGz: Copy() failed: %s", err.Error())
			}
		default:
			log.Fatalf(
				"ExtractTarGz: uknown type: %d in %s",
				header.Typeflag,
				header.Name)
		}
	}
}

func doTestStats(t *testing.T, cnt int, size int, fileName string) *StatsData {
	var durations []float64

	for i := 0; i < cnt; i++ {
		d, _ := doTest(t, size, fileName)
		durations = append(durations, float64(d))
	}

	mean, _ := stats.Mean(durations)
	stdDev, _ := stats.StandardDeviation(durations)
	stdDev = float64(100) / mean * stdDev

	clockTicks, err := sysconf.Sysconf(sysconf.SC_CLK_TCK)
	if err != nil {
		panic(err)
	}

	mean = mean * float64(1000) / float64(clockTicks) / float64(size)
	stdDevMs := stdDev / float64(100) * mean

	st := StatsData{
		meanMs:     mean,
		stdDevMs:   stdDevMs,
		stdDevPerc: stdDev,
	}

	fmt.Printf("loop %dx%d: sample time: %.2fms [deviation ±%.2fms, %.1f%%]\n", cnt, size, st.meanMs, st.stdDevMs, st.stdDevPerc)

	return &st
}

func checkPort(port int) bool {
	ln, err := net.Listen("tcp", ":"+fmt.Sprint(port))
	if err != nil {
		return false
	}

	_ = ln.Close()
	return true
}

func doTest(t *testing.T, iterations int, fileName string) (int64, error) {
	//lines, err := os.ReadFile("test.exporter-flags.txt")
	//if !assert.NoError(t, err, "unable to read exporter args file") {
	//	return 0, err
	//}

	var port = -1
	for i := portRangeStart; i < portRangeEnd; i++ {
		if checkPort(i) {
			port = i
			break
		}
	}

	if port == -1 {
		panic(fmt.Sprintf("Failed to find free port in range [%d..%d]", portRangeStart, portRangeEnd))
	}

	//linesStr := string(lines)
	linesStr := fmt.Sprintf("--web.listen-address=127.0.0.1:%d", port)
	linesArr := strings.Split(linesStr, "\n")

	cmd := exec.Command(fileName, linesArr...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "DATA_SOURCE_NAME='postgresql://postgres:postgres@127.0.0.1:5432/postgres_exporter?sslmode=disable'")

	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Start()
	if !assert.NoError(t, err, "Failed to start exporter. Process output:\n%q", out.String()) {
		return 0, err
	}

	err = waitForExporter(port)
	if !assert.NoError(t, err, "Failed to wait for exporter. Process output:\n%q", out.String()) {
		return 0, err
	}

	total1 := getCPUTime(cmd.Process.Pid)

	for i := 0; i < iterations; i++ {
		err = tryGetMetrics(port)
		if !assert.NoError(t, err) {
			return 0, err
		}

		time.Sleep(1 * time.Millisecond)
	}

	total2 := getCPUTime(cmd.Process.Pid)

	err = cmd.Process.Signal(unix.SIGINT)
	assert.NoError(t, err, "Failed to send SIGINT to exporter process")

	err = cmd.Wait()
	if err != nil && err.Error() != "signal: interrupt" {
		assert.NoError(t, err, "Failed to wait for exporter process termination. Process output:\n%q", out.String())
		return 0, err
	}

	return total2 - total1, nil
}

func getCPUTime(pid int) (total int64) {
	contents, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return
	}
	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		numFields := len(fields)
		if numFields > 3 {
			i, err := strconv.ParseInt(fields[13], 10, 64)
			if err != nil {
				panic(err)
			}

			totalTime := i

			i, err = strconv.ParseInt(fields[14], 10, 64)
			if err != nil {
				panic(err)
			}

			totalTime += i

			total = totalTime

			return
		}
	}
	return
}

func tryGetMetrics(port int) error {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	if err != nil {
		return fmt.Errorf("failed to get response from exporters web interface: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get response from exporters web interface: %w", err)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response from exporters web interface: %w", err)
	}

	bodyString := string(bodyBytes)
	if bodyString == "" {
		return fmt.Errorf("got empty response from exporters web interface: %w", err)
	}

	err = resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to close response body: %w", err)
	}

	return nil
}

func waitForExporter(port int) error {
	watchdog := exporterWaitTimeoutMs

	for ; tryGetMetrics(port) != nil && watchdog > 0; watchdog-- {
		time.Sleep(1 * time.Millisecond)
	}

	if watchdog == 0 {
		return fmt.Errorf("Failed to wait for exporter (on port %d)", port)
	}

	return nil
}
