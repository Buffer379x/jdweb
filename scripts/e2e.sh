#!/usr/bin/env bash
# Browser end-to-end tests of the built image (Playwright, test/e2e).
#
#   scripts/e2e.sh            build the image, start it twice, run all tests, clean up
#   scripts/e2e.sh real       only the tests against the real JDownloader
#   scripts/e2e.sh fake       only the tests against the fake JDownloader
#
# Needs: the test stack (scripts/teststack.sh up), Docker, Go, Node with pnpm, and Chromium
# for Playwright (pnpm exec playwright install chromium; PLAYWRIGHT_BROWSERS_PATH is honoured).
# Environment: E2E_NO_BUILD=1 uses the existing image jdweb:e2e. Extra arguments after "--" go to Playwright.
#
# Containers it creates and removes again: jdweb-e2e (image against the real JDownloader),
# jdweb-e2e-fake (image against the fake) and jdweb-e2e-fakejd (the fake JDownloader).
# It never touches anything else, and the tests refuse a JDownloader that holds packages they did not create.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="jdweb:e2e"
NETWORK="jdweb-test-net"
APP="jdweb-e2e"
FAKE_APP="jdweb-e2e-fake"
FAKE_JD="jdweb-e2e-fakejd"
JD_CONTAINER="jdweb-test-jd"
APP_PORT=17468
FAKE_APP_PORT=17469
FAKE_CTL_PORT=17470

log() { printf '[e2e] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

what="all"
case "${1:-}" in real | fake | all) what="$1"; shift ;; esac
[ "${1:-}" = "--" ] && shift

cleanup() {
  docker rm -f "$APP" "$FAKE_APP" "$FAKE_JD" >/dev/null 2>&1 || true
  [ -z "${TMPBIN:-}" ] || rm -rf "$TMPBIN"
}
trap 'rc=$?; cleanup; exit $rc' EXIT

docker container inspect "$JD_CONTAINER" >/dev/null 2>&1 || die "the test stack is not running: scripts/teststack.sh up"
docker network inspect "$NETWORK" >/dev/null 2>&1 || die "network $NETWORK is missing: scripts/teststack.sh up"
cleanup

if [ -z "${E2E_NO_BUILD:-}" ]; then
  log "building $IMAGE"
  docker build -q -t "$IMAGE" "$ROOT" >/dev/null
fi

wait_http() { # wait_http <url>
  for _ in $(seq 1 60); do
    curl -fsS "$1" >/dev/null 2>&1 && return 0
    sleep 0.5
  done
  die "$1 does not answer"
}

if [ "$what" != "fake" ]; then
  log "starting $APP against the real JDownloader"
  docker run -d --name "$APP" --network "$NETWORK" -p "127.0.0.1:${APP_PORT}:7468" \
    -e JDWEB_JD_URL="http://${JD_CONTAINER}:3128" "$IMAGE" >/dev/null
  wait_http "http://127.0.0.1:${APP_PORT}/healthz"
fi

if [ "$what" != "real" ]; then
  log "starting the fake JDownloader and $FAKE_APP"
  TMPBIN="$(mktemp -d)"
  arch="$(docker version -f '{{.Server.Arch}}')"
  (cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$TMPBIN/fakejd" ./internal/testjd/cmd/fakejd)
  chmod 755 "$TMPBIN" "$TMPBIN/fakejd"
  docker run -d --name "$FAKE_JD" --network "$NETWORK" -p "127.0.0.1:${FAKE_CTL_PORT}:3129" \
    -v "$TMPBIN/fakejd:/fakejd:ro" --entrypoint /fakejd "$IMAGE" \
    -addr 0.0.0.0:3128 -ctl 0.0.0.0:3129 -config 2000 >/dev/null
  wait_http "http://127.0.0.1:${FAKE_CTL_PORT}/state"
  docker run -d --name "$FAKE_APP" --network "$NETWORK" -p "127.0.0.1:${FAKE_APP_PORT}:7468" \
    -e JDWEB_JD_URL="http://${FAKE_JD}:3128" "$IMAGE" >/dev/null
  wait_http "http://127.0.0.1:${FAKE_APP_PORT}/healthz"
fi

cd "$ROOT/test/e2e"
[ -d node_modules ] || pnpm install --frozen-lockfile
proj=""
[ "$what" = "all" ] || proj="$what"
export E2E_URL="http://127.0.0.1:${APP_PORT}" E2E_FAKE_URL="http://127.0.0.1:${FAKE_APP_PORT}" E2E_FAKE_CTL="http://127.0.0.1:${FAKE_CTL_PORT}"
pnpm exec playwright test ${proj:+--project="$proj"} "$@"
