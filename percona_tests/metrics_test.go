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

var dumpMetricsFlag = flag.Bool("dump-metrics", false, "")
var printExtraMetrics = flag.Bool("extra-metrics", false, "")
var printMultipleLabels = flag.Bool("multiple-labels", false, "")

func TestMetrics(t *testing.T) {
	// put postgres_exporter and postgres_exporter_percona files in 'percona' folder
	// or use TestPrepareExporters to download exporters from feature build
	if doRun == nil || !*doRun {
		//t.Skip("For manual runs only through make")
		//return
	}

	newMetrics, err := getMetrics("../percona_tests/postgres_exporter")
	if err != nil {
		t.Error(err)
		return
	}

	oldMetrics, err := getMetrics("../percona_tests/postgres_exporter_percona")
	if err != nil {
		t.Error(err)
		return
	}

	newMetricsArr := strings.Split(newMetrics, "\n")
	oldMetricsArr := strings.Split(oldMetrics, "\n")

	oldMetricsArr = getMetricNames(oldMetricsArr)
	newMetricsArr = getMetricNames(newMetricsArr)

	if dumpMetricsFlag != nil && *dumpMetricsFlag {
		dumpMetrics(oldMetricsArr, newMetricsArr)
	}

	oldMetricsData := parseMetrics(oldMetricsArr)
	newMetricsData := parseMetrics(newMetricsArr)

	//newMetricsData = append(newMetricsData[:3], newMetricsData[40:]...)

	oldMetr := groupByMetrics(oldMetricsData)
	newMetr := groupByMetrics(newMetricsData)

	extraMetrics := make([]string, 0)
	for key := range newMetr {
		if _, ok := oldMetr[key]; !ok {
			extraMetrics = append(extraMetrics, key)
		}
	}
	sort.Strings(extraMetrics)

	missingMetrics := make([]string, 0)
	for key := range oldMetr {
		if _, ok := newMetr[key]; !ok {
			missingMetrics = append(missingMetrics, key)
		}
	}
	sort.Strings(missingMetrics)

	missingMetricLabels := make(map[string]string)
	missingMetricLabelsNames := make([]string, 0)
	for metric, labels := range oldMetr {
		if metric == "postgres_exporter_build_info" || metric == "go_info" {
			continue
		}

		if _, ok := newMetr[metric]; ok {
			newLabels := newMetr[metric]
			if !arrIsSubsetOf(labels, newLabels) {
				missingMetricLabels[metric] = fmt.Sprintf("    expected: %s\n    actual:   %s", labels, newLabels)
				missingMetricLabelsNames = append(missingMetricLabelsNames, metric)
			}
		}
	}
	sort.Strings(missingMetricLabelsNames)

	metricsWithMultipleLabels := make(map[string][]string)
	for k, v := range newMetr {
		if len(v) > 1 {
			found := false
			for i := 0; !found && i < len(v); i++ {
				lbl := v[i]
				for j := 0; j < len(v); j++ {
					if i == j {
						continue
					}

					lbl1 := v[j]
					if strings.Contains(lbl, lbl1) || strings.Contains(lbl1, lbl) {
						found = true
						break
					}
				}
			}
			if found {
				metricsWithMultipleLabels[k] = v
			}
		}
	}

	if getBool(printExtraMetrics) && len(extraMetrics) > 0 {
		fmt.Printf("Extra metrics (%d items):\n    %s\n\n", len(extraMetrics), strings.Join(extraMetrics, "\n    "))
	}

	if getBool(printMultipleLabels) && len(metricsWithMultipleLabels) > 0 {
		ss := make([]string, 0)
		for k, v := range metricsWithMultipleLabels {
			ss = append(ss, fmt.Sprintf("%s\n        %s", k, strings.Join(v, "\n        ")))
		}
		fmt.Printf("Some metrics were collected multiple times with extra labels (%d items):\n    %s\n\n", len(metricsWithMultipleLabels), strings.Join(ss, "\n    "))
	}

	if len(missingMetrics) > 0 {
		t.Errorf("Missing metrics:\n%s", strings.Join(missingMetrics, "\n"))
	}

	if len(missingMetricLabelsNames) > 0 {
		ll := make([]string, 0)
		for _, metric := range missingMetricLabelsNames {
			labels := missingMetricLabels[metric]
			ll = append(ll, metric+"\n"+labels)
		}

		t.Errorf("Missing metric's labels (%d metrics):\n%s", len(missingMetricLabelsNames), strings.Join(ll, "\n"))
	}
}

func getBool(val *bool) bool {
	return val != nil && *val
}

func arrIsSubsetOf(a, b []string) bool {
	if len(a) == 0 {
		return false
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
	metricsData := make([]Metric, 0)
	for i := 0; i < len(metrics); i++ {
		str := metrics[i]
		if str == "" || strings.HasPrefix(str, "# ") {
			continue
		}

		var mName, mLabels string
		if strings.Contains(str, "{") {
			mName = str[:strings.Index(str, "{")]
			mLabels = str[strings.Index(str, "{")+1 : len(str)-1]
		} else {
			mName = str
		}

		mstr := Metric{
			name:   mName,
			labels: mLabels,
		}

		metricsData = append(metricsData, mstr)
	}

	return metricsData
}

type Metric struct {
	name   string
	labels string
}

func dumpMetrics(oldMetricsArr, newMetricsArr []string) {
	f, _ := os.Create("metrics.old.txt")
	for _, s := range oldMetricsArr {
		f.WriteString(s)
		f.WriteString("\n")
	}
	f.Close()

	f, _ = os.Create("metrics.new.txt")
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
