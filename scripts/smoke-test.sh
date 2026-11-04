#!/usr/bin/env bash
# Smoke test for the JDWeb Docker image.
#
#   scripts/smoke-test.sh <image>
#
# Checks: image size, start as 99:100 on a read-only file system, health,
# security headers, web app, setup with the token from the log, login, status
# without a session, restart with data, clean stop, and the Unraid way: started
# as root on a root-owned data folder, JDWeb gives it to PUID:PGID and drops root.
#
# Containers and volumes made here start with "jdweb-smoke-" and are removed at
# the end. Nothing else is touched. Needs docker, curl and a helper image with
# `stat` (alpine, set SMOKE_HELPER to change it).
set -euo pipefail

IMAGE=${1:?usage: smoke-test.sh <image>}
ID=$$
NAME=jdweb-smoke-$ID
VOL=jdweb-smoke-$ID
PORT=${SMOKE_PORT:-18468}
PORT2=$((PORT + 1))
HELPER=${SMOKE_HELPER:-alpine:3.24.2}
TMP=$(mktemp -d)
PASSWORD='smoke-test-password-1'
LOGIN_BODY='{"username":"smoke","password":"smoke-test-password-1"}'
ROOT_DIR=
ROOT_VOL=

fail() {
  echo "FAIL: $*" >&2
  for c in "$NAME" "$NAME-root" "$NAME-noperm"; do
    if docker inspect "$c" >/dev/null 2>&1; then echo "--- log of $c" >&2; docker logs "$c" 2>&1 | tail -20 >&2 || true; fi
  done
  exit 1
}
cleanup() {
  docker rm -f "$NAME" "$NAME-root" "$NAME-noperm" >/dev/null 2>&1 || true
  docker volume rm "$VOL" "$ROOT_VOL" >/dev/null 2>&1 || true
  # Files in the bind mounts belong to 99:100: remove them from inside.
  for d in "$ROOT_DIR" "$TMP/noperm"; do
    [ -n "$d" ] && [ -d "$d" ] && docker run --rm -v "$d:/d" "$HELPER" sh -c 'rm -rf /d/* /d/.[!.]*' >/dev/null 2>&1 || true
  done
  rm -rf "$TMP" 2>/dev/null || true
}
trap cleanup EXIT

# User id of the main process, as docker top shows it (a number: the image has no passwd).
top_user() {
  local out
  out=$(docker top "$1" -o pid,uid 2>/dev/null) || out=$(docker top "$1" -o pid,user) # ps of some VMs has no uid column
  awk 'NR==2{print $2}' <<<"$out"
}
wait_healthy() { # name seconds
  for _ in $(seq 1 "$2"); do
    [ "$(docker inspect -f '{{.State.Health.Status}}' "$1")" = healthy ] && return 0
    [ "$(docker inspect -f '{{.State.Running}}' "$1")" = true ] || return 1
    sleep 1
  done
  return 1
}
http_code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
# POST JSON the way the web app does: Origin of the host and the request header.
post() { # port path json [curl args...]
  local port=$1 route=$2 body=$3; shift 3
  curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    -H "Origin: http://127.0.0.1:$port" -H 'X-JDWeb-Request: 1' --data "$body" "$@" "http://127.0.0.1:$port$route"
}
setup_account() { # container port
  local token code body
  token=$(docker logs "$1" 2>&1 | sed -n 's/.*"setup_token":"\([^"]*\)".*/\1/p' | sed -n 1p)
  [ -n "$token" ] || fail "no setup token in the log of $1"
  body='{"token":"'$token'","username":"smoke","password":"'$PASSWORD'"}'
  code=$(post "$2" /api/v1/setup "$body")
  [ "$code" = 204 ] || fail "setup answered HTTP $code"
}
owner_of() { # container-or-volume-mount-arg paths...  -> "path uid:gid" lines
  local mount=$1; shift
  docker run --rm -v "$mount:/d:ro" "$HELPER" stat -c '%n %u:%g' "$@"
}
mib() { # "12.5MiB" -> 12
  awk -v s="$1" 'BEGIN{n=s+0; if(s~/GiB/)n*=1024; else if(s~/KiB/)n/=1024; else if(s~/kB/)n/=1024; print int(n)}'
}

echo "== image size"
SIZE=$(docker image inspect "$IMAGE" --format '{{.Size}}')
echo "   $((SIZE / 1024 / 1024)) MB ($SIZE bytes)"
[ "$SIZE" -lt $((40 * 1024 * 1024)) ] || fail "image is larger than 40 MB"

echo "== start as 99:100, read-only, no capabilities"
[ "$(docker image inspect "$IMAGE" --format '{{.Config.User}}')" = 99:100 ] || fail "image user is not 99:100"
docker run -d --name "$NAME" --read-only --cap-drop ALL --security-opt no-new-privileges:true \
  --health-interval 2s --health-start-period 1s -p "127.0.0.1:$PORT:7468" -v "$VOL:/data" "$IMAGE" >/dev/null
wait_healthy "$NAME" 40 || fail "container does not become healthy"
VER=$(docker exec "$NAME" /jdweb version) || fail "jdweb version"
[ -n "$VER" ] || fail "empty version"
[ ! -f VERSION ] || [ "$VER" = "$(cat VERSION)" ] || fail "binary says $VER, VERSION file says $(cat VERSION)"
echo "   version $VER"
U=$(top_user "$NAME")
[ "$U" = 99 ] || fail "runs as '$U', not 99"
[ "$(curl -fsS "http://127.0.0.1:$PORT/healthz")" = ok ] || fail "/healthz is not ok"

echo "== web app and security headers"
curl -fsS -D "$TMP/h" -o "$TMP/index.html" "http://127.0.0.1:$PORT/" || fail "GET / failed"
grep -q '<div id="app">' "$TMP/index.html" || fail "index.html is not the web app"
ASSET=$(grep -o '/assets/[^"]*\.js' "$TMP/index.html" | sed -n 1p)
[ -n "$ASSET" ] || fail "index.html names no script"
[ "$(http_code "http://127.0.0.1:$PORT$ASSET")" = 200 ] || fail "script $ASSET not served"
[ "$(http_code "http://127.0.0.1:$PORT/fonts/barlow-latin-400-normal.woff2")" = 200 ] || fail "font not served"
for url in / /api/v1/setup/status /api/v1/status; do
  curl -s -D "$TMP/h" -o /dev/null "http://127.0.0.1:$PORT$url"
  grep -qi '^content-security-policy: default-src .none.' "$TMP/h" || fail "$url: no Content-Security-Policy"
  grep -qi '^x-content-type-options: nosniff' "$TMP/h" || fail "$url: no X-Content-Type-Options"
  grep -qi '^referrer-policy: no-referrer' "$TMP/h" || fail "$url: no Referrer-Policy"
done

echo "== no session: 401; setup with the token from the log; login"
[ "$(http_code "http://127.0.0.1:$PORT/api/v1/status")" = 401 ] || fail "/api/v1/status without a session is not 401"
[ "$(curl -s "http://127.0.0.1:$PORT/api/v1/setup/status")" = '{"required":true,"reset":false}' ] || fail "setup is not required on a fresh data folder"
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -H "Origin: http://127.0.0.1:$PORT" --data '{}' "http://127.0.0.1:$PORT/api/v1/login")" = 403 ] \
  || fail "login without X-JDWeb-Request is not 403"
[ "$(post "$PORT" /api/v1/setup '{"token":"wrong","username":"smoke","password":"smoke-test-password-1"}')" = 400 ] || fail "wrong setup token is not 400"
setup_account "$NAME" "$PORT"
[ "$(post "$PORT" /api/v1/login "$LOGIN_BODY" -c "$TMP/jar")" = 204 ] || fail "login failed"
grep -q jdweb_session "$TMP/jar" || fail "no session cookie"
[ "$(http_code -b "$TMP/jar" "http://127.0.0.1:$PORT/api/v1/status")" = 200 ] || fail "/api/v1/status with a session is not 200"
STATUS=$(curl -s -b "$TMP/jar" "http://127.0.0.1:$PORT/api/v1/status")
[[ "$STATUS" == *'"connection"'* ]] || fail "status has no connection field"
SESSION=$(curl -s -b "$TMP/jar" "http://127.0.0.1:$PORT/api/v1/session")
[[ "$SESSION" == *'"username":"smoke"'* ]] || fail "session does not name the user"
echo "   ok (JDownloader is not reachable here, so the connection is reported as down; that is expected)"

echo "== memory at idle (after setup and login, which hash passwords)"
sleep 5
MEM=$(docker stats --no-stream --format '{{.MemUsage}}' "$NAME" | cut -d/ -f1 | tr -d ' ')
echo "   $MEM"
[ "$(mib "$MEM")" -lt 50 ] || fail "idle memory $MEM is above 50 MiB"

echo "== clean stop, restart keeps the data"
START=$(date +%s)
docker stop -t 10 "$NAME" >/dev/null
DUR=$(( $(date +%s) - START ))
echo "   stopped in ${DUR}s"
[ "$DUR" -lt 6 ] || fail "stop took ${DUR}s, so SIGTERM was not handled"
[ "$(docker inspect -f '{{.State.ExitCode}}' "$NAME")" = 0 ] || fail "exit code is $(docker inspect -f '{{.State.ExitCode}}' "$NAME"), not 0"
LOG=$(docker logs "$NAME" 2>&1)
[[ "$LOG" == *'"shutting down"'* ]] || fail "no shutdown message in the log"
docker start "$NAME" >/dev/null
wait_healthy "$NAME" 40 || fail "not healthy after restart"
[ "$(curl -s "http://127.0.0.1:$PORT/api/v1/setup/status")" = '{"required":false,"reset":false}' ] || fail "setup is open again after restart"
[ "$(post "$PORT" /api/v1/login "$LOGIN_BODY")" = 204 ] || fail "login after restart failed"
docker rm -f "$NAME" >/dev/null

echo "== data folder that is not writable (started as 99:100): clear message"
mkdir -p "$TMP/noperm" && chmod 755 "$TMP/noperm"
if [ "$(uname -s)" = Linux ]; then
  docker run --rm --name "$NAME-noperm" -v "$TMP/noperm:/data" "$IMAGE" >"$TMP/noperm.log" 2>&1 && fail "started with a data folder it cannot write" || true
  grep -q 'not writable' "$TMP/noperm.log" || { cat "$TMP/noperm.log" >&2; fail "no 'not writable' message"; }
  grep -q 'chown -R 99:100' "$TMP/noperm.log" || fail "the message does not name the chown command"
else
  echo "   skipped: bind mounts on $(uname -s) do not enforce Linux file permissions"
fi

echo "== Unraid way: --user 0:0, read-only, root-owned data folder"
# A bind mount on Linux; elsewhere a volume, because Docker Desktop and OrbStack do not keep ownership in bind mounts.
if [ "$(uname -s)" = Linux ]; then
  ROOT_DIR=$TMP/rootdata; mkdir -p "$ROOT_DIR"
  MOUNT=$ROOT_DIR
  docker run --rm -v "$ROOT_DIR:/d" "$HELPER" chown 0:0 /d
else
  ROOT_VOL=jdweb-smoke-root-$ID
  docker volume create "$ROOT_VOL" >/dev/null
  MOUNT=$ROOT_VOL
  docker run --rm -v "$ROOT_VOL:/d" "$HELPER" chown 0:0 /d
fi
[ "$(owner_of "$MOUNT" /d | cut -d' ' -f2)" = 0:0 ] || fail "the data folder is not root-owned before the start"
docker run -d --name "$NAME-root" --user 0:0 --read-only --security-opt no-new-privileges:true --memory=256m \
  --health-interval 2s --health-start-period 1s -p "127.0.0.1:$PORT2:7468" -v "$MOUNT:/data" "$IMAGE" >/dev/null
wait_healthy "$NAME-root" 40 || fail "root start: not healthy"
LOG=$(docker logs "$NAME-root" 2>&1)
[[ "$LOG" == *'"dropped root"'* ]] || fail "root start: no 'dropped root' in the log"
U=$(top_user "$NAME-root")
[ "$U" = 99 ] || fail "root start: runs as '$U', not 99"
setup_account "$NAME-root" "$PORT2"
# Maintenance as root (docker exec) must not leave root-owned files either.
docker exec "$NAME-root" /jdweb reset-password >/dev/null 2>&1 || fail "reset-password as root failed"
BAD=$(owner_of "$MOUNT" /d /d/jdweb.db /d/jdweb.db-wal /d/jdweb.db-shm | grep -v ' 99:100$' || true)
[ -z "$BAD" ] || fail "files are not owned by 99:100: $BAD"
[ "$(curl -fsS "http://127.0.0.1:$PORT2/healthz")" = ok ] || fail "root start: /healthz is not ok"
[ "$(curl -s "http://127.0.0.1:$PORT2/api/v1/setup/status")" = '{"required":true,"reset":true}' ] || fail "reset token does not reopen the setup page as a reset"
START=$(date +%s)
docker stop -t 10 "$NAME-root" >/dev/null
[ $(( $(date +%s) - START )) -lt 6 ] || fail "root start: slow stop"
[ "$(docker inspect -f '{{.State.ExitCode}}' "$NAME-root")" = 0 ] || fail "root start: exit code is not 0"

echo "Smoke test passed."
