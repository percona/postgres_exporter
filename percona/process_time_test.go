package upstream_update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/tklauser/go-sysconf"
	"golang.org/x/sys/unix"
)

const (
	postgresHost     = "127.0.0.1"
	postgresPort     = 5432
	postgresUser     = "postgres"
	postgresPassword = "postgres"

	exporterWaitTimeoutMs = 3000 // time to wait for exporter process start

	portRangeStart = 20000 // exporter web interface listening port
	portRangeEnd   = 20100 // exporter web interface listening port

	repeatCount  = 5
	scrapesCount = 20
)

var doRun = flag.Bool("doRun", false, "")
var url = flag.String("url", "", "")

type StatsData struct {
	meanMs     float64
	stdDevMs   float64
	stdDevPerc float64

	meanHwm        float64
	stdDevHwmBytes float64
	stdDevHwmPerc  float64

	meanData        float64
	stdDevDataBytes float64
	stdDevDataPerc  float64
}

func TestPerformance(t *testing.T) {
	tr := true
	doRun = &tr
	// put postgres_exporter and postgres_exporter_percona files in 'percona' folder
	// or use TestPrepareExporters to download exporters from feature build
	if doRun == nil || !*doRun {
		t.Skip("For manual runs only through make")
		return
	}

	var updated, original *StatsData
	t.Run("upstream exporter", func(t *testing.T) {
		updated = doTestStats(t, repeatCount, scrapesCount, "../percona/postgres_exporter")
	})

	t.Run("percona exporter", func(t *testing.T) {
		original = doTestStats(t, repeatCount, scrapesCount, "../percona/postgres_exporter_percona")
	})

	diff := original.meanMs - updated.meanMs
	diffPerc := float64(100) / math.Min(original.meanMs, updated.meanMs) * diff
	var diffLabel string
	if diff > 0 {
		diffLabel = "faster"
	} else {
		diffLabel = "slower"
	}

	fmt.Println()
	fmt.Printf("Updated exporter is %.0f %% %s (%.2f ms)\n", diffPerc, diffLabel, diff)
	fmt.Println()
}

// TestPrepareExporters extracts exporter from client binary's tar.gz
func TestPrepareUpdatedExporter(t *testing.T) {
	if doRun == nil || !*doRun {
		t.Skip("For manual runs only through make")
		return
	}

	if url == nil || *url == "" {
		t.Error("URL not defined")
		return
	}

	prepareExporter(*url, "postgres_exporter")
}

// TestPrepareExporters extracts exporter from client binary's tar.gz
func TestPreparePerconaExporter(t *testing.T) {
	if doRun == nil || !*doRun {
		t.Skip("For manual runs only through make")
		return
	}

	if url == nil || *url == "" {
		t.Error("URL not defined")
		return
	}

	prepareExporter(*url, "postgres_exporter_percona")
}

func prepareExporter(url, fileName string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}

	defer resp.Body.Close()

	extractExporter(resp.Body, fileName)

	err = exec.Command("chmod", "+x", fileName).Run()
	if err != nil {
		log.Fatal(err)
	}
}

func extractExporter(gzipStream io.Reader, fileName string) {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		log.Fatal("ExtractTarGz: NewReader failed")
	}

	tarReader := tar.NewReader(uncompressedStream)

	exporterFound := false
	for !exporterFound {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			log.Fatalf("ExtractTarGz: Next() failed: %s", err.Error())
		}

		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			if strings.HasSuffix(header.Name, "postgres_exporter") {
				outFile, err := os.Create(fileName)
				if err != nil {
					log.Fatalf("ExtractTarGz: Create() failed: %s", err.Error())
				}
				defer outFile.Close()
				if _, err := io.Copy(outFile, tarReader); err != nil {
					log.Fatalf("ExtractTarGz: Copy() failed: %s", err.Error())
				}

				exporterFound = true
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
	var hwms []float64
	var datas []float64

	for i := 0; i < cnt; i++ {
		d, hwm, data, err := doTest(size, fileName)
		if !assert.NoError(t, err) {
			return nil
		}

		durations = append(durations, float64(d))
		hwms = append(hwms, float64(hwm))
		datas = append(datas, float64(data))
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

	meanHwm, _ := stats.Mean(hwms)
	stdDevHwm, _ := stats.StandardDeviation(hwms)
	stdDevHwmPerc := float64(100) / meanHwm * stdDevHwm

	meanData, _ := stats.Mean(datas)
	stdDevData, _ := stats.StandardDeviation(datas)
	stdDevDataPerc := float64(100) / meanData * stdDevData

	st := StatsData{
		meanMs:     mean,
		stdDevMs:   stdDevMs,
		stdDevPerc: stdDev,

		meanHwm:        meanHwm,
		stdDevHwmBytes: stdDevHwm,
		stdDevHwmPerc:  stdDevHwmPerc,

		meanData:        meanData,
		stdDevDataBytes: stdDevData,
		stdDevDataPerc:  stdDevDataPerc,
	}

	//fmt.Printf("loop %dx%d: sample time: %.2fms [deviation ±%.2fms, %.1f%%]\n", cnt, scrapesCount, st.meanMs, st.stdDevMs, st.stdDevPerc)
	fmt.Printf("running %d scrapes %d times\n", size, cnt)
	fmt.Printf("CPU time: %.2fms [±%.2fms, %.1f%%]\n", st.meanMs, st.stdDevMs, st.stdDevPerc)
	fmt.Printf("VmHWM: %.2f kB [±%.2f kB, %.1f%%]\n", st.meanHwm, st.stdDevHwmBytes, st.stdDevHwmPerc)
	fmt.Printf("VmData: %.2f kB [±%.2f kB, %.1f%%]\n", st.meanData, st.stdDevDataBytes, st.stdDevDataPerc)

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

func doTest(iterations int, fileName string) (cpu, hwm, data int64, _ error) {
	lines, err := os.ReadFile("test.exporter-flags.txt")
	if err != nil {
		return 0, 0, 0, errors.Wrapf(err, "Unable to read exporter args file")
	}

	var port = -1
	for i := portRangeStart; i < portRangeEnd; i++ {
		if checkPort(i) {
			port = i
			break
		}
	}

	if port == -1 {
		return 0, 0, 0, errors.Wrapf(err, "Failed to find free port in range [%d..%d]", portRangeStart, portRangeEnd)
	}

	linesStr := string(lines)
	linesStr += fmt.Sprintf("\n--web.listen-address=127.0.0.1:%d", port)

	absolutePath, _ := filepath.Abs("custom-queries")
	linesStr += fmt.Sprintf("\n--collect.custom_query.hr.directory=%s/high-resolution", absolutePath)
	linesStr += fmt.Sprintf("\n--collect.custom_query.mr.directory=%s/medium-resolution", absolutePath)
	linesStr += fmt.Sprintf("\n--collect.custom_query.lr.directory=%s/low-resolution", absolutePath)

	linesArr := strings.Split(linesStr, "\n")

	dsn := fmt.Sprintf("DATA_SOURCE_NAME=postgresql://%s:%s@%s:%d/postgres?sslmode=disable", postgresUser, postgresPassword, postgresHost, postgresPort)

	cmd := exec.Command(fileName, linesArr...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, dsn)

	var outBuffer, errorBuffer bytes.Buffer
	cmd.Stdout = &outBuffer
	cmd.Stderr = &errorBuffer

	collectOutput := func() string {
		result := ""
		outStr := outBuffer.String()
		if outStr == "" {
			result = "Process stdOut was empty. "
		} else {
			result = fmt.Sprintf("Process stdOut:\n%s\n", outStr)
		}
		errStr := errorBuffer.String()
		if errStr == "" {
			result += "Process stdErr was empty."
		} else {
			result += fmt.Sprintf("Process stdErr:\n%s\n", errStr)
		}

		return result
	}

	err = cmd.Start()
	if err != nil {
		return 0, 0, 0, errors.Wrapf(err, "Failed to start exporter.%s", collectOutput())
	}

	err = waitForExporter(port)
	if err != nil {
		return 0, 0, 0, errors.Wrapf(err, "Failed to wait for exporter.%s", collectOutput())
	}

	total1 := getCPUTime(cmd.Process.Pid)

	for i := 0; i < iterations; i++ {
		err = tryGetMetrics(port)
		if err != nil {
			return 0, 0, 0, errors.Wrapf(err, "Failed to perform test iteration %d.%s", i, collectOutput())
		}

		time.Sleep(1 * time.Millisecond)
	}

	total2 := getCPUTime(cmd.Process.Pid)

	hwm, data = getCPUMem(cmd.Process.Pid)

	err = cmd.Process.Signal(unix.SIGINT)
	if err != nil {
		return 0, 0, 0, errors.Wrapf(err, "Failed to send SIGINT to exporter process.%s\n", collectOutput())
	}

	err = cmd.Wait()
	if err != nil && err.Error() != "signal: interrupt" {
		return 0, 0, 0, errors.Wrapf(err, "Failed to wait for exporter process termination.%s\n", collectOutput())
	}

	return total2 - total1, hwm, data, nil
}

func getCPUMem(pid int) (hwm, data int64) {
	contents, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0
	}

	lines := strings.Split(string(contents), "\n")

	for _, v := range lines {
		if strings.HasPrefix(v, "VmHWM") {
			val := strings.ReplaceAll(strings.ReplaceAll(strings.Split(v, ":\t")[1], " kB", ""), " ", "")
			hwm, _ = strconv.ParseInt(val, 10, 64)
			continue
		}
		if strings.HasPrefix(v, "VmData") {
			val := strings.ReplaceAll(strings.ReplaceAll(strings.Split(v, ":\t")[1], " kB", ""), " ", "")
			data, _ = strconv.ParseInt(val, 10, 64)
			continue
		}
	}

	return hwm, data
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
	uri := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
	client := new(http.Client)

	request, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return err
	}
	request.Header.Add("Accept-Encoding", "gzip")

	response, err := client.Do(request)

	if err != nil {
		return fmt.Errorf("failed to get response from exporters web interface: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get response from exporters web interface: %w", err)
	}

	// Check that the server actually sent compressed data
	var reader io.ReadCloser
	enc := response.Header.Get("Content-Encoding")
	switch enc {
	case "gzip":
		reader, err = gzip.NewReader(response.Body)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer reader.Close()
	default:
		reader = response.Body
	}

	buf := new(strings.Builder)
	_, err = io.Copy(buf, reader)
	if err != nil {
		return err
	}

	rr := buf.String()
	if rr == "" {
		return fmt.Errorf("failed to read response")
	}

	err = response.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to close response: %w", err)
	}

	return nil
}

func waitForExporter(port int) error {
	watchdog := exporterWaitTimeoutMs

	for ; tryGetMetrics(port) != nil && watchdog > 0; watchdog-- {
		time.Sleep(1 * time.Millisecond)
		if watchdog < 1000 {
			time.Sleep(1 * time.Millisecond)
		}
	}

	if watchdog == 0 {
		return fmt.Errorf("failed to wait for exporter (on port %d)", port)
	}

	return nil
}
