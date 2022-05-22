package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
)

//go:embed percona-reference-metrics.txt
var referenceMetrics string

// TestReferenceCompatibility checks that exposed metrics are not missed.
//
// Used to make sure that metrics are present after updating from upstream.
// You need you run exporter locally on port 42002.
func TestReferenceCompatibility(t *testing.T) {
	resp, err := http.Get("http://localhost:42002/metrics")
	assert.Nil(t, err)
	defer resp.Body.Close()
	currentMetricsBytes, err := ioutil.ReadAll(resp.Body)
	assert.Nil(t, err)

	//scanner := bufio.NewScanner(strings.NewReader("pg_settings_pg_stat_statements_save{collector=\"exporter\",server=\"127.0.0.1:5432\"} 0\n"))

	currentMetrics := toMap(t, string(currentMetricsBytes))
	referenceMetrics := toMap(t, referenceMetrics)

	//remove matches
	for m, _ := range currentMetrics {
		_, found := referenceMetrics[m]
		if found {
			delete(referenceMetrics, m)
			delete(currentMetrics, m)
		}
	}

	fmt.Printf("Extra metrics [%d]:\n", len(currentMetrics))
	for _, metric := range sortedKeys(currentMetrics) {
		fmt.Printf("\t%s\n", metric)
	}
	if len(referenceMetrics) != 0 {
		fmt.Printf("Not Supported metrics [%d]:\n", len(referenceMetrics))
		for _, metric := range sortedKeys(referenceMetrics) {
			fmt.Printf("\t%s\n", metric)
		}
		assert.FailNowf(t, "Found not supported metrics", "Count: %d", len(referenceMetrics))
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func toMap(t *testing.T, rawMetrics string) map[string]string {
	result := make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(rawMetrics))
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		next := scanner.Text()
		isComment := strings.HasPrefix(next, "#")
		if isComment {
			continue
		}
		next = cleanKeyOrValue(next)
		items := strings.Split(next, " ")
		assert.Equal(t, len(items), 2) // metric and value
		result[items[0]] = items[1]
	}

	return result
}

var floatRegExp = regexp.MustCompile(`[+-]?(\d*[.])?\d+(e[+-]?\d*)?`)
var ipRegExp = regexp.MustCompile(`\d*\.\d*\.\d*\.\d*:\d*`)
var goExp = regexp.MustCompile(`go1.\d*.\d*`)
var removeAttr1 = "PostgreSQL 11.15 (Debian 11.15-1.pgdg90+1) on x86_64-pc-linux-gnu, compiled by gcc (Debian 6.3.0-18+deb9u1) 6.3.0 20170516, 64-bit"
var removeAttr2 = "PostgreSQL 11.16 (Debian 11.16-1.pgdg90+1) on x86_64-pc-linux-gnu, compiled by gcc (Debian 6.3.0-18+deb9u1) 6.3.0 20170516, 64-bit"
var removeAttr3 = "collector=\"exporter\","
var removeAttr4 = "fastpath function call"
var removeAttr5 = "idle in transaction (aborted)"
var removeAttr6 = "idle in transaction"
var removeAttr7 = "+Inf"
var removeAttr8 = "0.0.1"

func cleanKeyOrValue(s string) (res string) {
	res = s
	res = ipRegExp.ReplaceAllString(res, "-")
	res = goExp.ReplaceAllString(res, "-")
	res = strings.ReplaceAll(res, removeAttr1, "")
	res = strings.ReplaceAll(res, removeAttr2, "")
	res = strings.ReplaceAll(res, removeAttr3, "")
	res = strings.ReplaceAll(res, removeAttr4, "")
	res = strings.ReplaceAll(res, removeAttr5, "")
	res = strings.ReplaceAll(res, removeAttr6, "")
	res = strings.ReplaceAll(res, removeAttr7, "")
	res = strings.ReplaceAll(res, removeAttr8, "")
	res = floatRegExp.ReplaceAllString(res, "-")
	return
}
