FROM busybox:1.37.0-uclibc AS health-tools
FROM otel/opentelemetry-collector-contrib:0.155.0
COPY --from=health-tools /bin/busybox /busybox
