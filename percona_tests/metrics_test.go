package percona_tests

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

var dumpMetricsFlag = flag.Bool("dumpMetrics", false, "")
var printExtraMetrics = flag.Bool("extraMetrics", false, "")
var printMultipleLabels = flag.Bool("multipleLabels", false, "")

type Metric struct {
	name   string
	labels string
}

type MetricsCollection struct {
	RawMetricStr    string
	RawMetricStrArr []string
	MetricNames     []string
	MetricsData     []Metric
	LabelsByMetric  map[string][]string
}

func TestMetrics(t *testing.T) {
	// put postgres_exporter and postgres_exporter_percona files in 'percona' folder
	// or use TestPrepareExporters to download exporters from feature build
	if !getBool(doRun) {
		t.Skip("For manual runs only through make")
		return
	}

	newMetrics, err := getMetrics("assets/postgres_exporter")
	if err != nil {
		t.Error(err)
		return
	}

	oldMetrics, err := getMetrics("assets/postgres_exporter_percona")
	if err != nil {
		t.Error(err)
		return
	}

	oldMetricsCollection := parseMetricsCollection(oldMetrics)
	newMetricsCollection := parseMetricsCollection(newMetrics)

	if getBool(dumpMetricsFlag) {
		dumpMetrics(oldMetricsCollection.RawMetricStrArr, newMetricsCollection.RawMetricStrArr)
	}

	if getBool(printExtraMetrics) {
		dumpExtraMetrics(newMetricsCollection, oldMetricsCollection)
	}

	if getBool(printMultipleLabels) {
		dumpMetricsWithMultipleLabelSets(newMetricsCollection)
	}

	t.Run("MissingMetricsTest", func(t *testing.T) {
		if ok, msg := testForMissingMetrics(oldMetricsCollection, newMetricsCollection); !ok {
			t.Error(msg)
		}
	})

	t.Run("MissingMetricsLabelsTest", func(t *testing.T) {
		if ok, msg := testForMissingMetricsLabels(oldMetricsCollection, newMetricsCollection); !ok {
			t.Error(msg)
		}
	})
}

func testForMissingMetricsLabels(oldMetricsCollection, newMetricsCollection MetricsCollection) (bool, string) {
	missingMetricLabels := make(map[string]string)
	missingMetricLabelsNames := make([]string, 0)
	for metric, labels := range oldMetricsCollection.LabelsByMetric {
		// skip version info label mismatch
		if metric == "postgres_exporter_build_info" || metric == "go_info" {
			continue
		}

		if _, ok := newMetricsCollection.LabelsByMetric[metric]; ok {
			newLabels := newMetricsCollection.LabelsByMetric[metric]
			if !arrIsSubsetOf(labels, newLabels) {
				missingMetricLabels[metric] = fmt.Sprintf("    expected: %s\n    actual:   %s", labels, newLabels)
				missingMetricLabelsNames = append(missingMetricLabelsNames, metric)
			}
		}
	}
	sort.Strings(missingMetricLabelsNames)

	if len(missingMetricLabelsNames) > 0 {
		ll := make([]string, 0)
		for _, metric := range missingMetricLabelsNames {
			labels := missingMetricLabels[metric]
			ll = append(ll, metric+"\n"+labels)
		}

		return false, fmt.Sprintf("Missing metric's labels (%d metrics):\n%s", len(missingMetricLabelsNames), strings.Join(ll, "\n"))
	}

	return true, ""
}

func testForMissingMetrics(oldMetricsCollection, newMetricsCollection MetricsCollection) (bool, string) {
	missingMetrics := make([]string, 0)
	for metricName := range oldMetricsCollection.LabelsByMetric {
		if _, ok := newMetricsCollection.LabelsByMetric[metricName]; !ok {
			missingMetrics = append(missingMetrics, metricName)
		}
	}
	sort.Strings(missingMetrics)
	if len(missingMetrics) > 0 {
		return false, fmt.Sprintf("Missing metrics:\n%s", strings.Join(missingMetrics, "\n"))
	}

	return true, ""
}

func dumpMetricsWithMultipleLabelSets(newMetricsCollection MetricsCollection) {
	metricsWithMultipleLabels := make(map[string][]string)
	for metricName, newMetricLabels := range newMetricsCollection.LabelsByMetric {
		if len(newMetricLabels) > 1 {
			found := false
			for i := 0; !found && i < len(newMetricLabels); i++ {
				lbl := newMetricLabels[i]

				for j := 0; j < len(newMetricLabels); j++ {
					if i == j {
						continue
					}

					lbl1 := newMetricLabels[j]
					if lbl == "" || lbl1 == "" {
						continue
					}

					if strings.Contains(lbl, lbl1) || strings.Contains(lbl1, lbl) {
						found = true
						break
					}
				}
			}
			if found {
				metricsWithMultipleLabels[metricName] = newMetricLabels
			}
		}
	}

	if len(metricsWithMultipleLabels) > 0 {
		ss := make([]string, 0, len(metricsWithMultipleLabels))
		for k, v := range metricsWithMultipleLabels {
			ss = append(ss, fmt.Sprintf("%s\n        %s", k, strings.Join(v, "\n        ")))
		}
		fmt.Printf("Some metrics were collected multiple times with extra labels (%d items):\n    %s\n\n", len(metricsWithMultipleLabels), strings.Join(ss, "\n    "))
	}
}

func dumpExtraMetrics(newMetricsCollection, oldMetricsCollection MetricsCollection) {
	extraMetrics := make([]string, 0)
	for metricName := range newMetricsCollection.LabelsByMetric {
		if _, ok := oldMetricsCollection.LabelsByMetric[metricName]; !ok {
			extraMetrics = append(extraMetrics, metricName)
		}
	}
	sort.Strings(extraMetrics)

	if len(extraMetrics) > 0 {
		fmt.Printf("Extra metrics (%d items):\n    %s\n\n", len(extraMetrics), strings.Join(extraMetrics, "\n    "))
	}
}

func parseMetricsCollection(metricRaw string) MetricsCollection {
	rawMetricsArr := strings.Split(metricRaw, "\n")
	metricNamesArr := getMetricNames(rawMetricsArr)
	metrics := parseMetrics(metricNamesArr)
	labelsByMetrics := groupByMetrics(metrics)

	return MetricsCollection{
		MetricNames:     metricNamesArr,
		MetricsData:     metrics,
		RawMetricStr:    metricRaw,
		RawMetricStrArr: rawMetricsArr,
		LabelsByMetric:  labelsByMetrics,
	}
}

func getBool(val *bool) bool {
	return val != nil && *val
}

func arrIsSubsetOf(a, b []string) bool {
	if len(a) == 0 {
		return len(b) == 0
	}

	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}

	return true
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}

	return false
}

// groupByMetrics returns labels grouped by metric
func groupByMetrics(metrics []Metric) map[string][]string {
	mtr := make(map[string][]string)

	for i := 0; i < len(metrics); i++ {
		metric := metrics[i]
		if _, ok := mtr[metric.name]; ok {
			labels := mtr[metric.name]
			labels = append(labels, metric.labels)
			mtr[metric.name] = labels
		} else {
			mtr[metric.name] = []string{metric.labels}
		}
	}

	return mtr
}

func parseMetrics(metrics []string) []Metric {
	metricsLength := len(metrics)
	metricsData := make([]Metric, 0, metricsLength)
	for i := 0; i < metricsLength; i++ {
		metricRawStr := metrics[i]
		if metricRawStr == "" || strings.HasPrefix(metricRawStr, "# ") {
			continue
		}

		var mName, mLabels string
		if strings.Contains(metricRawStr, "{") {
			mName = metricRawStr[:strings.Index(metricRawStr, "{")]
			mLabels = metricRawStr[strings.Index(metricRawStr, "{")+1 : len(metricRawStr)-1]
		} else {
			mName = metricRawStr
		}

		metric := Metric{
			name:   mName,
			labels: mLabels,
		}

		metricsData = append(metricsData, metric)
	}

	return metricsData
}

func dumpMetrics(oldMetricsArr, newMetricsArr []string) {
	f, _ := os.Create("assets/metrics.old.txt")
	for _, s := range oldMetricsArr {
		f.WriteString(s)
		f.WriteString("\n")
	}
	f.Close()

	f, _ = os.Create("assets/metrics.new.txt")
	for _, s := range newMetricsArr {
		f.WriteString(s)
		f.WriteString("\n")
	}
	f.Close()
}

func getMetricNames(metrics []string) []string {
	length := len(metrics)
	ret := make([]string, length)
	for i := 0; i < length; i++ {
		str := metrics[i]
		if str == "" || strings.HasPrefix(str, "# ") {
			ret[i] = str
			continue
		}

		idx := strings.LastIndex(str, " ")
		if idx >= 0 {
			str1 := str[:idx]
			ret[i] = str1
		} else {
			ret[i] = str
		}
	}

	return ret
}

func getMetrics(fileName string) (string, error) {
	cmd, port, collectOutput, err := launchExporter(fileName)
	if err != nil {
		return "", errors.Wrap(err, "Failed to launch exporter")
	}

	metrics, err := tryGetMetrics(port)
	if err != nil {
		return "", errors.Wrap(err, "Failed to get metrics")
	}

	err = stopExporter(cmd, collectOutput)
	if err != nil {
		return "", errors.Wrap(err, "Failed to stop exporter")
	}

	return metrics, nil
}
