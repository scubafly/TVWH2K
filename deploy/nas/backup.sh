#!/usr/bin/env bash
# Dated pg_dump (custom format) of the app DB via docker exec. No credentials here:
# runs inside the container as the postgres user (local socket auth) and reads
# POSTGRES_DB / BACKUP_* from .env.selfhost (or the environment).
set -euo pipefail

cd "$(dirname "$0")"
ENV_FILE="${ENV_FILE:-.env.selfhost}"
if [ -f "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  set -a; . "$ENV_FILE"; set +a
fi

CONTAINER="${PG_CONTAINER:-tvwh2k-pg}"
DB="${POSTGRES_DB:-tvwh2k}"
DIR="${BACKUP_DIR:-./backups}"
KEEP="${BACKUP_RETENTION_DAYS:-14}"

umask 077
mkdir -p "$DIR"
chmod 700 "$DIR"
OUT="$DIR/${DB}_$(date +%Y%m%d_%H%M%S).dump"
TMP="$OUT.partial"

docker exec "$CONTAINER" pg_dump -U postgres -d "$DB" -Fc > "$TMP"
mv "$TMP" "$OUT"
chmod 600 "$OUT"
echo "backup written: $OUT"

find "$DIR" -maxdepth 1 -name "${DB}_*.dump" -type f -mtime +"$KEEP" -print -delete

# Restore (into an existing, empty DB; stop the API first):
#   docker compose --env-file .env.selfhost -f compose.selfhost.yml stop tvwh2k gotrue
#   docker exec -i tvwh2k-pg pg_restore -U postgres -d tvwh2k --clean --if-exists --no-owner < FILE.dump
#   docker compose --env-file .env.selfhost -f compose.selfhost.yml start gotrue tvwh2k
# Cron (daily 03:15):
#   15 3 * * * /volume1/docker/tvwh2k/deploy/nas/backup.sh >> /volume1/docker/tvwh2k/backup.log 2>&1
