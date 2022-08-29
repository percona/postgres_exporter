test-performance:
	go test -v -run '^TestPerformance$\' -args -doRun=true

test-metrics:
	go test -v -run '^TestMetrics$\' -args -doRun=true

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
