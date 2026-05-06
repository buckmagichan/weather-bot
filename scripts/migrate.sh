#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$ROOT_DIR/migrations}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-weather-bot-postgres}"
WAIT_SECONDS="${WAIT_SECONDS:-30}"
DIRECTION="${1:-up}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") [up|down]

Environment overrides:
  ENV_FILE             Path to the env file. Default: $ROOT_DIR/.env
  MIGRATIONS_DIR       Path to SQL migrations. Default: $ROOT_DIR/migrations
  POSTGRES_CONTAINER   Docker container name. Default: weather-bot-postgres
  WAIT_SECONDS         Seconds to wait for Postgres to become ready. Default: 30
USAGE
}

if [[ "${DIRECTION}" == "-h" || "${DIRECTION}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ "${DIRECTION}" != "up" && "${DIRECTION}" != "down" ]]; then
  usage >&2
  exit 2
fi

if [[ -f "${ENV_FILE}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
fi

: "${POSTGRES_DB:?POSTGRES_DB is required. Set it in ${ENV_FILE} or the environment.}"
: "${POSTGRES_USER:?POSTGRES_USER is required. Set it in ${ENV_FILE} or the environment.}"

if ! docker ps -a --format '{{.Names}}' | grep -qx "${POSTGRES_CONTAINER}"; then
  cat >&2 <<ERROR
Postgres container '${POSTGRES_CONTAINER}' does not exist.
Start it with:
  docker compose --env-file ${ENV_FILE} -f ${ROOT_DIR}/infra/docker-compose.postgres.yml up -d
ERROR
  exit 1
fi

echo "Waiting for Postgres container '${POSTGRES_CONTAINER}' to be ready..."
for ((i = 1; i <= WAIT_SECONDS; i++)); do
  container_status="$(docker inspect -f '{{.State.Status}}' "${POSTGRES_CONTAINER}")"
  if [[ "${container_status}" == "running" ]] && \
    docker exec "${POSTGRES_CONTAINER}" pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; then
    break
  fi

  if [[ "${i}" -eq "${WAIT_SECONDS}" ]]; then
    cat >&2 <<ERROR
Postgres container '${POSTGRES_CONTAINER}' is not ready after ${WAIT_SECONDS}s.
Current container status: ${container_status}

Check why Postgres is not staying up:
  docker compose --env-file ${ENV_FILE} -f ${ROOT_DIR}/infra/docker-compose.postgres.yml logs postgres
ERROR
    exit 1
  fi

  sleep 1
done

if [[ ! -d "${MIGRATIONS_DIR}" ]]; then
  echo "Migrations directory not found: ${MIGRATIONS_DIR}" >&2
  exit 1
fi

declare -a migration_files=()
if [[ "${DIRECTION}" == "up" ]]; then
  while IFS= read -r migration_file; do
    migration_files+=("${migration_file}")
  done < <(find "${MIGRATIONS_DIR}" -maxdepth 1 -type f -name "*.up.sql" | sort)
else
  while IFS= read -r migration_file; do
    migration_files+=("${migration_file}")
  done < <(find "${MIGRATIONS_DIR}" -maxdepth 1 -type f -name "*.down.sql" | sort -r)
fi

if [[ "${#migration_files[@]}" -eq 0 ]]; then
  echo "No ${DIRECTION} migrations found in ${MIGRATIONS_DIR}."
  exit 0
fi

for migration_file in "${migration_files[@]}"; do
  echo "Applying $(basename "${migration_file}")..."
  docker exec -i "${POSTGRES_CONTAINER}" \
    psql -v ON_ERROR_STOP=1 -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
    < "${migration_file}"
done

echo "Done."
