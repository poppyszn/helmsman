# Backup sidecar for the home pilot: pg_dump to an S3 compatible bucket on a schedule, plus
# list, restore and a restore drill. Not needed when backups run from host cron (deploy/backup/backup.sh).
FROM postgres:16-alpine@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685
RUN apk add --no-cache aws-cli tzdata
COPY deploy/backup/sidecar.sh /usr/local/bin/backup
RUN chmod +x /usr/local/bin/backup
USER postgres
ENTRYPOINT ["/usr/local/bin/backup"]
CMD ["loop"]
