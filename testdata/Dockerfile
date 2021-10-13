ARG POSTGRESQL_IMAGE

FROM ${POSTGRESQL_IMAGE}


COPY testdata/ssl /ssl
RUN chown 999:999 /ssl/* \
    && chmod 0600 /ssl/*
