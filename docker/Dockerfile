FROM postgres:9.6
LABEL "Product"="PostgreSQL (SSL enabled)"
COPY server.key /var/lib/postgresql/server.key
COPY server.crt /var/lib/postgresql/server.crt
COPY rootCA.key /var/lib/postgresql/rootCA.key
COPY rootCA.crt /var/lib/postgresql/rootCA.crt
RUN chown postgres /var/lib/postgresql/server.key && \
    chmod 600 /var/lib/postgresql/server.key && \
    chown postgres /var/lib/postgresql/rootCA.key && \
    chmod 600 /var/lib/postgresql/rootCA.key
