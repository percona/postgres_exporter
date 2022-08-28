test-performance:
	go test -v process_time_test.go -run TestPerformance -args -doRun=false

# download exporter from provided feature build's client binary url
prepare-exporter:
	go test -v process_time_test.go -run TestPrepareUpdatedExporter -args -doRun=true -url=$(url)

prepare-base-exporter:
	go test -v process_time_test.go -run TestPreparePerconaExporter -args -doRun=true -url="https://github.com/percona/postgres_exporter/releases/download/v0.4.7/postgres_exporter_linux_amd64.tar.gz"

start-postgres-db:
	docker-compose -f postgres-compose.yml up -d --force-recreate --renew-anon-volumes --remove-orphans

stop-postgres-db:
	docker-compose -f postgres-compose.yml down
