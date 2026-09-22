FROM busybox:1.37.0-uclibc AS health-tools
FROM jaegertracing/all-in-one:1.68.0
COPY --from=health-tools /bin/busybox /busybox
