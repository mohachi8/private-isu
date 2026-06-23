#!/bin/bash
# Fast PDCA loop: build -> restart app -> short benchmark -> print score.
# Usage: bin/iter.sh [duration] [-m]
#   duration  benchmark length (default 25s; use 60s for an "official" run)
#   -m        also sample per-process/per-core metrics during the run
# Env: TARGET (default http://localhost), BENCH_TASKSET (e.g. "taskset -c 1")
# Does NOT restart MySQL (keeps the buffer pool warm).
set -uo pipefail
export PATH="$PATH:/home/isucon/.local/go/bin"
BIN_DIR=/home/isucon/private_isu/bin
DUR="25s"
METRICS=0
for a in "$@"; do
  case "$a" in
    -m) METRICS=1 ;;
    *) DUR="$a" ;;
  esac
done
TARGET="${TARGET:-http://localhost}"

cd /home/isucon/private_isu/webapp/golang
if ! go build -o app; then
  echo "BUILD FAILED"; exit 1
fi
sudo systemctl restart isu-go

# poll the app directly until it returns 200 (startup loads the caches first)
up=0
for i in $(seq 1 60); do
  code=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/ 2>/dev/null || true)
  if [ "$code" = "200" ]; then up=1; break; fi
  sleep 0.5
done
[ "$up" = "1" ] || { echo "APP NOT UP"; exit 1; }

sudo truncate -s 0 /var/log/nginx/access.log /var/log/mysql/mysql-slow.log 2>/dev/null || true

MPID=""
if [ "$METRICS" = "1" ]; then
  "$BIN_DIR/metrics.sh" "$(echo "$DUR" | sed 's/s$//')" > /tmp/iter_metrics.txt 2>&1 &
  MPID=$!
fi

cd /home/isucon/private_isu/benchmarker
${BENCH_TASKSET:-} ./bin/benchmarker -t "$TARGET" -u ./userdata -benchmark-timeout "$DUR" 2>&1 \
  | grep -oE '"(pass|score|fail)":[a-z0-9]+' | tr '\n' ' '
echo

if [ -n "$MPID" ]; then
  wait "$MPID" 2>/dev/null
  echo "===== metrics ====="; cat /tmp/iter_metrics.txt
fi
