#!/usr/bin/env bash
# Nightly backup: a consistent Postgres dump + the MinIO object tree.
# Idempotent, safe to run by hand at any time.
set -euo pipefail

REPO="${REPO:-/srv/opsflow}"
DEST="${DEST:-/var/backups/opsflow}"
RETAIN_DAYS="${RETAIN_DAYS:-14}"
PROJECT=opsflow                     # compose project name -> volume prefix
TS="$(date +%Y%m%d-%H%M)"

mkdir -p "$DEST/db" "$DEST/files"

# -- database: pg_dump in custom format, restorable with pg_restore ------------
# Run inside the container so the password never appears on a command line.
docker compose -f "$REPO/docker-compose.yml" exec -T -e PGPASSWORD -u postgres postgres \
    pg_dump -U opsflow -d opsflow -Fc --no-owner \
    > "$DEST/db/opsflow-$TS.dump"
printf 'db   %s (%s)\n' "opsflow-$TS.dump" "$(du -h "$DEST/db/opsflow-$TS.dump" | cut -f1)"

# -- uploaded files: tar the MinIO volume --------------------------------------
docker run --rm \
    -v "${PROJECT}_miniodata:/data:ro" \
    -v "$DEST/files:/out" \
    alpine:3.21 tar -C /data -czf "/out/files-$TS.tar.gz" .
printf 'files %s (%s)\n' "files-$TS.tar.gz" "$(du -h "$DEST/files/files-$TS.tar.gz" | cut -f1)"

# -- prune ---------------------------------------------------------------------
find "$DEST/db"    -name 'opsflow-*.dump'  -mtime "+$RETAIN_DAYS" -print -delete
find "$DEST/files" -name 'files-*.tar.gz'  -mtime "+$RETAIN_DAYS" -print -delete

printf 'kept: %s dumps, %s file archives, %s total\n' \
    "$(ls -1 "$DEST/db" | wc -l)" "$(ls -1 "$DEST/files" | wc -l)" "$(du -sh "$DEST" | cut -f1)"
