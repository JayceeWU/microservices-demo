FROM busybox:1.37.0-uclibc AS health-tools
FROM prom/prometheus:v3.5.0
COPY --from=health-tools /bin/busybox /busybox
