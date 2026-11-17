FROM alpine AS builder
RUN apk add --no-cache ca-certificates

FROM scratch AS final
ARG TARGETPLATFORM
# GoReleaser stages the binaries under $TARGETPLATFORM in its build context.
ARG BIN_DIR=$TARGETPLATFORM
USER 65535:65535
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY $BIN_DIR/postgres_exporter /
EXPOSE 9187
ENTRYPOINT ["/postgres_exporter"]
