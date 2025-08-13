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
var endpointFlag = flag.String("endpoint", "", "")

const highResolutionEndpoint = "metrics?collect%5B%5D=custom_query.hr&collect%5B%5D=exporter&collect%5B%5D=standard.go&collect%5B%5D=standard.process"
const medResolutionEndpoint = "metrics?collect%5B%5D=custom_query.mr"
const lowResolutionEndpoint = "metrics?collect%5B%5D=custom_query.lr"

// that metric is disabled by default in new exporters, so will trigger test
// however we don't use it at all in our dashboards, so for now - safe to skip it
var skipMetricNames = []string{"go_memstats_gc_cpu_fraction", "go_info"}

type Metric struct {
	name             string
	labelsRawStr     string
	labelsWithValues []string
}

type MetricsCollection struct {
	RawMetricStr          string
	RawMetricStrArr       []string
	MetricNamesWithLabels []string
	MetricsData           []Metric
	LabelsByMetric        map[string][]string
}

func TestMissingMetrics(t *testing.T) {
	if !getBool(doRun) {
		t.Skip("For manual runs only through make")
		return
	}

	endpoint := "metrics?collect[]=exporter&collect[]=postgres&collect[]=custom_query.mr"
	newMetrics, err := getMetricsFrom(updatedExporterFileName, updatedExporterArgs, endpoint)
	if err != nil {
		t.Error(err)
		return
	}

	oldMetrics, err := getMetricsFrom(oldExporterFileName, oldExporterArgs, endpoint)
	if err != nil {
		t.Error(err)
		return
	}

	err = os.WriteFile(updatedExporterMetrics, []byte(newMetrics), os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(oldExporterMetrics, []byte(oldMetrics), os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}

	oldMetricsCollection := parseMetricsCollection(oldMetrics)
	newMetricsCollection := parseMetricsCollection(newMetrics)

	if ok, msg := testForMissingMetrics(oldMetricsCollection, newMetricsCollection); !ok {
		t.Error(msg)
	}
}

func TestMissingLabels(t *testing.T) {
	if !getBool(doRun) {
		t.Skip("For manual runs only through make")
		return
	}

	newMetrics, err := getMetrics(updatedExporterFileName, updatedExporterArgs)
	if err != nil {
		t.Error(err)
		return
	}

	oldMetrics, err := getMetrics(oldExporterFileName, oldExporterArgs)
	if err != nil {
		t.Error(err)
		return
	}

	err = os.WriteFile(updatedExporterMetrics+"-labels", []byte(newMetrics), os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(oldExporterMetrics+"-labels", []byte(oldMetrics), os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}

	oldMetricsCollection := parseMetricsCollection(oldMetrics)
	newMetricsCollection := parseMetricsCollection(newMetrics)

	if ok, msg := testForMissingMetricsLabels(oldMetricsCollection, newMetricsCollection); !ok {
		t.Error(msg)
	}
}

func TestDumpMetrics(t *testing.T) {
	if !getBool(doRun) {
		t.Skip("For manual runs only through make")
		return
	}

	var ep string
	switch *endpointFlag {
	case "hr":
		ep = highResolutionEndpoint
	case "mr":
		ep = medResolutionEndpoint
	case "lr":
		ep = lowResolutionEndpoint
	default:
		ep = "metrics"
	}

	newMetrics, err := getMetricsFrom(updatedExporterFileName, updatedExporterArgs, ep)
	if err != nil {
		t.Error(err)
		return
	}

	oldMetrics, err := getMetricsFrom(oldExporterFileName, oldExporterArgs, ep)
	if err != nil {
		t.Error(err)
		return
	}

	oldMetricsCollection := parseMetricsCollection(oldMetrics)
	newMetricsCollection := parseMetricsCollection(newMetrics)

	dumpMetricsInfo(oldMetricsCollection, newMetricsCollection)
}

func TestResolutionsMetricDuplicates(t *testing.T) {
	if !getBool(doRun) {
		t.Skip("For manual runs only through make")
		return
	}

	hrMetrics, err := getMetricsFrom(updatedExporterFileName, updatedExporterArgs, highResolutionEndpoint)
	if err != nil {
		t.Error(err)
		return
	}

	mrMetrics, err := getMetricsFrom(updatedExporterFileName, updatedExporterArgs, medResolutionEndpoint)
	if err != nil {
		t.Error(err)
		return
	}

	lrMetrics, err := getMetricsFrom(updatedExporterFileName, updatedExporterArgs, lowResolutionEndpoint)
	if err != nil {
		t.Error(err)
		return
	}

	hrMetricsColl := parseMetricsCollection(hrMetrics)
	mrMetricsColl := parseMetricsCollection(mrMetrics)
	lrMetricsColl := parseMetricsCollection(lrMetrics)

	ms := make(map[string][]string)
	addMetrics(ms, hrMetricsColl.MetricNamesWithLabels, "HR")
	addMetrics(ms, mrMetricsColl.MetricNamesWithLabels, "MR")
	addMetrics(ms, lrMetricsColl.MetricNamesWithLabels, "LR")

	count := 0
	msg := ""
	for metric, resolutions := range ms {
		if len(resolutions) > 1 {
			count++
			msg += fmt.Sprintf("'%s' is duplicated in %s\n", metric, resolutions)
		}
	}

	if count > 0 {
		t.Errorf("Found %d duplicated metrics:\n%s", count, msg)
	}
}

func addMetrics(ms map[string][]string, metrics []string, resolution string) {
	for _, m := range metrics {
		if m == "" || strings.HasPrefix(m, "# ") {
			continue
		}

		ms[m] = append(ms[m], resolution)
	}
}

func TestResolutions(t *testing.T) {
	if !getBool(doRun) {
		t.Skip("For manual runs only through make")
		return
	}

	t.Run("TestLowResolution", func(t *testing.T) {
		testResolution(t, lowResolutionEndpoint, "Low")
	})

	t.Run("TestMediumResolution", func(t *testing.T) {
		testResolution(t, medResolutionEndpoint, "Medium")
	})

	t.Run("TestHighResolution", func(t *testing.T) {
		testResolution(t, highResolutionEndpoint, "High")
	})
}

func testResolution(t *testing.T, resolutionEp, resolutionName string) {
	newMetrics, err := getMetricsFrom(updatedExporterFileName, updatedExporterArgs, resolutionEp)
	if err != nil {
		t.Error(err)
		return
	}

	oldMetrics, err := getMetricsFrom(oldExporterFileName, oldExporterArgs, resolutionEp)
	if err != nil {
		t.Error(err)
		return
	}

	err = os.WriteFile(fmt.Sprintf("%s-%s", updatedExporterMetrics, resolutionName), []byte(newMetrics), os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(fmt.Sprintf("%s-%s", oldExporterMetrics, resolutionName), []byte(oldMetrics), os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}

	oldMetricsCollection := parseMetricsCollection(oldMetrics)
	newMetricsCollection := parseMetricsCollection(newMetrics)

	missingCount := 0
	missingMetrics := ""
	missingLabelsCount := 0
	missingLabels := ""
	for _, oldMetric := range oldMetricsCollection.MetricsData {
		// skip empty lines, comments and redundant metrics
		if oldMetric.name == "" || strings.HasPrefix(oldMetric.name, "# ") {
			continue
		}
		if skipMetric(oldMetric.name) {
			continue
		}

		metricFound := false
		labelsMatch := false
		for _, newMetric := range newMetricsCollection.MetricsData {
			if newMetric.name != oldMetric.name {
				continue
			}

			metricFound = true

			if newMetric.labelsRawStr == oldMetric.labelsRawStr {
				labelsMatch = true
				break
			}

			if arrIsSubsetOf(oldMetric.labelsWithValues, newMetric.labelsWithValues) {
				labelsMatch = true
				break
			}
		}

		if !metricFound {
			missingCount++
			missingMetrics += fmt.Sprintf("%s\n", oldMetric)
		} else if !labelsMatch {
			missingLabelsCount++
			missingLabels += fmt.Sprintf("%s\n", oldMetric)
		}
	}

	if missingCount > 0 {
		t.Errorf("%d metric(s) are missing in new exporter for %s resolution:\n%s", missingCount, resolutionName, missingMetrics)
	}

	if missingLabelsCount > 0 {
		t.Errorf("%d metrics's labels missing in new exporter for %s resolution:\n%s", missingLabelsCount, resolutionName, missingLabels)
	}

	extraCount := 0
	extraMetrics := ""
	for _, metric := range newMetricsCollection.MetricNamesWithLabels {
		if metric == "" || strings.HasPrefix(metric, "# ") {
			continue
		}

		if !contains(oldMetricsCollection.MetricNamesWithLabels, metric) {
			extraCount++
			extraMetrics += fmt.Sprintf("%s\n", metric)
		}
	}
	if extraCount > 0 {
		fmt.Printf("[WARN] %d metrics are redundant in new exporter for %s resolution\n%s", extraCount, resolutionName, extraMetrics)
	}
}

func skipMetric(oldMetricName string) bool {
	skip := false
	for _, name := range skipMetricNames {
		if name == oldMetricName {
			skip = true
		}
	}
	return skip
}

func dumpMetricsInfo(oldMetricsCollection, newMetricsCollection MetricsCollection) {
	if getBool(dumpMetricsFlag) {
		dumpMetrics(oldMetricsCollection, newMetricsCollection)
	}

	if getBool(printExtraMetrics) {
		dumpExtraMetrics(newMetricsCollection, oldMetricsCollection)
	}

	if getBool(printMultipleLabels) {
		dumpMetricsWithMultipleLabelSets(newMetricsCollection)
	}
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
		if skipMetric(metricName) {
			continue
		}

		if _, ok := newMetricsCollection.LabelsByMetric[metricName]; !ok {
			missingMetrics = append(missingMetrics, metricName)
		}
	}
	sort.Strings(missingMetrics)
	if len(missingMetrics) > 0 {
		return false, fmt.Sprintf("Missing metrics (%d items):\n%s", len(missingMetrics), strings.Join(missingMetrics, "\n"))
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
		MetricNamesWithLabels: metricNamesArr,
		MetricsData:           metrics,
		RawMetricStr:          metricRaw,
		RawMetricStrArr:       rawMetricsArr,
		LabelsByMetric:        labelsByMetrics,
	}
}

func arrIsSubsetOf(smaller, larger []string) bool {
	if len(smaller) == 0 {
		return len(larger) == 0
	}

	for _, x := range smaller {
		if !contains(larger, x) {
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
			labels = append(labels, metric.labelsRawStr)
			mtr[metric.name] = labels
		} else {
			mtr[metric.name] = []string{metric.labelsRawStr}
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
		var labelsArr []string
		if strings.Contains(metricRawStr, "{") {
			mName = metricRawStr[:strings.Index(metricRawStr, "{")]
			mLabels = metricRawStr[strings.Index(metricRawStr, "{")+1 : len(metricRawStr)-1]
			if mLabels != "" {
				labelsArr = strings.Split(mLabels, ",")
			}
		} else {
			mName = metricRawStr
		}

		metric := Metric{
			name:             mName,
			labelsRawStr:     mLabels,
			labelsWithValues: labelsArr,
		}

		metricsData = append(metricsData, metric)
	}

	return metricsData
}

func dumpMetrics(oldMetrics, newMetrics MetricsCollection) {
	f, _ := os.Create("assets/metrics.old.txt")
	for _, s := range oldMetrics.RawMetricStrArr {
		f.WriteString(s)
		f.WriteString("\n")
	}
	f.Close()

	f, _ = os.Create("assets/metrics.new.txt")
	for _, s := range newMetrics.RawMetricStrArr {
		f.WriteString(s)
		f.WriteString("\n")
	}
	f.Close()

	f, _ = os.Create("assets/metrics.names.old.txt")
	for _, s := range oldMetrics.MetricNamesWithLabels {
		f.WriteString(s)
		f.WriteString("\n")
	}
	f.Close()
	f, _ = os.Create("assets/metrics.names.new.txt")
	for _, s := range newMetrics.MetricNamesWithLabels {
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

func getMetrics(fileName, argsFile string) (string, error) {
	return getMetricsFrom(fileName, argsFile, "metrics")
}

func getMetricsFrom(fileName, argsFile, endpoint string) (string, error) {
	cmd, port, collectOutput, err := launchExporter(fileName, argsFile)
	if err != nil {
		return "", errors.Wrap(err, "Failed to launch exporter")
	}

	metrics, err := tryGetMetricsFrom(port, endpoint)
	if err != nil {
		return "", errors.Wrap(err, "Failed to get metrics")
	}

	err = stopExporter(cmd, collectOutput)
	if err != nil {
		return "", errors.Wrap(err, "Failed to stop exporter")
	}

	return metrics, nil
}
