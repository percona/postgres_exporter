#########################
### tests

# measures avg scrape time and compares old vs new exporters
test-performance:
	go test -v -run '^TestPerformance$\' -args -doRun=true

extraMetrics = false
multipleLabels = false
dumpMetrics = false
# verifies metrics list old vs new and fails if some old metrics missing or labels on them missing
# optionally can dump any extra metrics or metrics collected multiple times with different labels
test-metrics:
	go test -v -run '^TestMissingMetrics$\' -args -doRun=true

test-labels:
	go test -v -run '^TestMissingLabels$\' -args -doRun=true

dump-metrics:
	go test -v -run '^TestDumpMetrics$\' -args -doRun=true -extraMetrics=$(extraMetrics) -multipleLabels=$(multipleLabels) -dumpMetrics=$(dumpMetrics)

#########################
### env preparation

# download exporter from provided feature build's client binary url
prepare-exporter:
	go test -v -run '^TestPrepareUpdatedExporter$\' -args -doRun=true -url=$(url)

prepare-exporter-from-repo:
	make -C ../ build && cp ../postgres_exporter assets/postgres_exporter

prepare-base-exporter:
	tar -xf assets/postgres_exporter_percona.tar.xz -C assets/

start-postgres-db:
	docker-compose -f assets/postgres-compose.yml up -d --force-recreate --renew-anon-volumes --remove-orphans

stop-postgres-db:
	docker-compose -f assets/postgres-compose.yml down

prepare-env-from-repo: prepare-exporter-from-repo prepare-base-exporter start-postgres-db
