#!/usr/bin/env bash
# Throwaway JDownloader test stack for JDWeb development and tests.
#
#   scripts/teststack.sh up     start (or repair) the stack; idempotent
#   scripts/teststack.sh down   remove everything this script created
#   scripts/teststack.sh url    print the JDownloader API base URL
#
# Containers: jdweb-test-jd (jlesage/jdownloader-2) and jdweb-test-files
# (fixture server, test/fixture/server.py). Both live in the network
# jdweb-test-net; JDownloader reaches the fixture at http://jdweb-test-files:8000.
# The JDownloader API is published on 127.0.0.1:13128 only.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

JD_CONTAINER="jdweb-test-jd"
FILES_CONTAINER="jdweb-test-files"
NETWORK="jdweb-test-net"
VOLUME="jdweb-test-config"
JD_IMAGE="jlesage/jdownloader-2:latest"
FIXTURE_IMAGE="python:3.13-alpine"
API_PORT="13128"
API_URL="http://127.0.0.1:${API_PORT}"
CFG_DIR="/config/cfg"
GENERAL_CFG="${CFG_DIR}/org.jdownloader.settings.GeneralSettings.json"
API_CFG="${CFG_DIR}/org.jdownloader.api.RemoteAPIConfig.json"
# The marker every test checks before it touches JDownloader (internal/testguard,
# test/e2e/support/guard.ts): a harmless setting that no other JDownloader has.
MARKER_CFG="${CFG_DIR}/org.jdownloader.api.myjdownloader.MyJDownloaderSettings.json"
MARKER_KEY="devicename"
MARKER_VALUE="jdweb-test-stack"
WAIT_SECONDS=240

log() { printf '[teststack] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

exists() { # exists <kind> <name>
  case "$1" in
    container) docker container inspect "$2" >/dev/null 2>&1 ;;
    network) docker network inspect "$2" >/dev/null 2>&1 ;;
    volume) docker volume inspect "$2" >/dev/null 2>&1 ;;
  esac
}

is_running() { [ "$(docker container inspect -f '{{.State.Running}}' "$1" 2>/dev/null || echo false)" = "true" ]; }

# Run a command in a throwaway helper container with the config volume at /config.
volume_helper() {
  docker run --rm -v "${VOLUME}:/config" "${FIXTURE_IMAGE}" "$@"
}

api_configured() {
  exists volume "${VOLUME}" || return 1
  volume_helper python -c '
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(1)
sys.exit(0 if d.get("deprecatedapienabled") is True and d.get("deprecatedapilocalhostonly") is False else 1)
' "${API_CFG}" >/dev/null 2>&1
}

marker_set() {
  exists volume "${VOLUME}" || return 1
  volume_helper python -c '
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(1)
sys.exit(0 if d.get(sys.argv[2]) == sys.argv[3] else 1)
' "${MARKER_CFG}" "${MARKER_KEY}" "${MARKER_VALUE}" >/dev/null 2>&1
}

# Merge the marker into its config file on the stopped config volume.
write_marker() {
  volume_helper python -c '
import json, os, sys
path = sys.argv[1]
try:
    data = json.load(open(path))
except (OSError, ValueError):
    data = {}
data[sys.argv[2]] = sys.argv[3]
os.makedirs(os.path.dirname(path), exist_ok=True)
with open(path, "w") as f:
    json.dump(data, f)
os.chown(path, 1000, 1000)
' "${MARKER_CFG}" "${MARKER_KEY}" "${MARKER_VALUE}"
}

ping_ok() {
  local out
  out="$(curl -fsS -m 3 -X POST "${API_URL}/device/ping" 2>/dev/null | tr -d ' \r\n')" || return 1
  [ "${out}" = '{"data":true}' ]
}

wait_for() { # wait_for <description> <command...>
  local what="$1"; shift
  local i
  for ((i = 0; i < WAIT_SECONDS; i++)); do
    if "$@"; then return 0; fi
    sleep 1
  done
  die "timeout (${WAIT_SECONDS}s) waiting for ${what}"
}

general_cfg_exists() {
  docker exec "${JD_CONTAINER}" test -f "${GENERAL_CFG}" >/dev/null 2>&1
}

run_jd() {
  docker run -d --name "${JD_CONTAINER}" \
    --network "${NETWORK}" \
    -p "127.0.0.1:${API_PORT}:3128" \
    -v "${VOLUME}:/config" \
    "${JD_IMAGE}" >/dev/null
}

run_fixture() {
  docker create --name "${FILES_CONTAINER}" --network "${NETWORK}" \
    "${FIXTURE_IMAGE}" python -u /server.py >/dev/null
  docker cp "${ROOT}/test/fixture/server.py" "${FILES_CONTAINER}:/server.py" >/dev/null \
    || { docker rm -f "${FILES_CONTAINER}" >/dev/null; die "cannot copy fixture server"; }
  docker start "${FILES_CONTAINER}" >/dev/null
}

# Merge the two API keys into RemoteAPIConfig.json on the stopped config volume.
enable_api() {
  volume_helper python -c '
import json, os, sys
path = sys.argv[1]
try:
    data = json.load(open(path))
except (OSError, ValueError):
    data = {}
data["deprecatedapienabled"] = True
data["deprecatedapilocalhostonly"] = False
os.makedirs(os.path.dirname(path), exist_ok=True)
with open(path, "w") as f:
    json.dump(data, f, indent=2)
os.chown(path, 1000, 1000)
' "${API_CFG}"
}

first_run_jd() {
  log "first start of JDownloader (initial config is generated)"
  exists container "${JD_CONTAINER}" && docker rm -f -v "${JD_CONTAINER}" >/dev/null
  exists volume "${VOLUME}" && docker volume rm "${VOLUME}" >/dev/null
  docker volume create "${VOLUME}" >/dev/null
  run_jd
  wait_for "${GENERAL_CFG}" general_cfg_exists
  log "initial config exists, stopping JDownloader to enable the API"
  docker stop -t 25 "${JD_CONTAINER}" >/dev/null
  enable_api
  write_marker
  docker start "${JD_CONTAINER}" >/dev/null
}

cmd_up() {
  command -v docker >/dev/null || die "docker not found"
  command -v curl >/dev/null || die "curl not found"
  exists network "${NETWORK}" || { log "creating network ${NETWORK}"; docker network create "${NETWORK}" >/dev/null; }

  if exists container "${FILES_CONTAINER}"; then
    is_running "${FILES_CONTAINER}" || docker start "${FILES_CONTAINER}" >/dev/null
  else
    log "starting fixture server"
    run_fixture
  fi

  if api_configured; then
    if ! marker_set; then
      # JDownloader writes its settings when it stops: change the file only while it is stopped.
      log "setting the test marker (JDownloader restarts once)"
      exists container "${JD_CONTAINER}" && is_running "${JD_CONTAINER}" && docker stop -t 25 "${JD_CONTAINER}" >/dev/null
      write_marker
    fi
    if ! exists container "${JD_CONTAINER}"; then
      run_jd
    elif ! is_running "${JD_CONTAINER}"; then
      docker start "${JD_CONTAINER}" >/dev/null
    fi
  else
    first_run_jd
  fi

  wait_for "JDownloader API (POST /device/ping)" ping_ok
  log "ready"
  printf '%s\n' "$(curl -fsS -m 3 -X POST "${API_URL}/device/ping")"
}

cmd_down() {
  local c
  for c in "${JD_CONTAINER}" "${FILES_CONTAINER}"; do
    if exists container "${c}"; then
      log "removing container ${c}"
      docker rm -f -v "${c}" >/dev/null
    fi
  done
  if exists network "${NETWORK}"; then
    log "removing network ${NETWORK}"
    docker network rm "${NETWORK}" >/dev/null
  fi
  if exists volume "${VOLUME}"; then
    log "removing volume ${VOLUME}"
    docker volume rm "${VOLUME}" >/dev/null
  fi
}

case "${1:-}" in
  up) cmd_up ;;
  down) cmd_down ;;
  url) printf '%s\n' "${API_URL}" ;;
  *) printf 'usage: %s up|down|url\n' "$0" >&2; exit 2 ;;
esac
