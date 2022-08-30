# measures avg scrape time and compares old vs new exporters
test-performance:
	go test -v -run '^TestPerformance$\' -args -doRun=true

extraMetrics = false
multipleLabels = false
# verifies metrics list old vs new and fails if some old metrics missing or labels on them missing
# optionally can dump any extra metrics or metrics collected multiple times with different labels
test-metrics:
	go test -v -run '^TestMetrics$\' -args -doRun=true -extraMetrics=$(extraMetrics) -multipleLabels=$(multipleLabels)

# env preparation

# download exporter from provided feature build's client binary url
prepare-exporter:
	go test -v env_prepare_test.go -run TestPrepareUpdatedExporter -args -doRun=true -url=$(url)

prepare-base-exporter:
	tar -xf postgres_exporter_percona.tar.xz

start-postgres-db:
	docker-compose -f postgres-compose.yml up -d --force-recreate --renew-anon-volumes --remove-orphans

stop-postgres-db:
	docker-compose -f postgres-compose.yml down
