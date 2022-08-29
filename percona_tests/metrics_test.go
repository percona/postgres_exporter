package percona_tests

import (
	"testing"
)

func TestMetrics(t *testing.T) {
	// put postgres_exporter and postgres_exporter_percona files in 'percona' folder
	// or use TestPrepareExporters to download exporters from feature build
	if doRun == nil || !*doRun {
		t.Skip("For manual runs only through make")
		return
	}

	cmd, _, collectOutput, err := launchExporter("../percona_tests/postgres_exporter")
	if err != nil {
		t.Error(err, "Failed to launch exporter")
		return
	}

	err = stopExporter(cmd, collectOutput)
	if err != nil {
		t.Error(err, "Failed to stop exporter")
		return
	}
}
