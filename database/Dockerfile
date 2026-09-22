FROM postgres:17-alpine
HEALTHCHECK NONE
COPY migrations /migrations
COPY seed /seed
COPY migrate.sh /migrate.sh
RUN chmod +x /migrate.sh
USER 65532:65532
ENTRYPOINT ["/migrate.sh"]
