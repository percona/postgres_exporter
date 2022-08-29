basic usage:

1.  download original exporter


    make prepare-base-exporter

2.a. download updated exporter

    make prepare-exporter url="<feature build client binary url>"

2.b. or use current repo as updated exporter

    make prepare-exporter-from-repo

3. start test postgres_server


    make start-postgres-db

4. run basic performance comparison test


    make test-performance
