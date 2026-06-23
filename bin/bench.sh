#!/bin/bash
# Rotate logs, run the benchmark with per-process/per-core metrics sampled in
# parallel, then print score/fail + metrics summary + alp/slow-query summaries.
#
# Target is configurable for moving to an off-box benchmarker:
#   TARGET=http://10.0.0.5 bin/bench.sh
# Optional: BENCH_TASKSET="taskset -c 1" pins the benchmarker to one core.
#           BENCH_TIMEOUT=90s overrides the run length.
set -uo pipefail
BIN_DIR=/home/isucon/private_isu/bin
TARGET="${TARGET:-${1:-http://localhost}}"
BENCH_DIR=/home/isucon/private_isu/benchmarker
DUR="${BENCH_TIMEOUT:-60s}"

echo "==> rotating logs"
sudo truncate -s 0 /var/log/nginx/access.log 2>/dev/null || true
sudo truncate -s 0 /var/log/mysql/mysql-slow.log 2>/dev/null || true

# sample per-process/per-core metrics in parallel for the benchmark window
metric_secs=$(echo "$DUR" | sed 's/s$//')
"$BIN_DIR/metrics.sh" "$metric_secs" > /tmp/bench_metrics.txt 2>&1 &
MPID=$!

echo "==> running benchmark against $TARGET (timeout $DUR)"
cd "$BENCH_DIR"
out=$(${BENCH_TASKSET:-} ./bin/benchmarker -t "$TARGET" -u ./userdata -benchmark-timeout "$DUR" 2>&1)
echo "$out" | grep -oE '"(pass|score|success|fail)":[a-z0-9]+' | tr '\n' ' '; echo
echo "$out" | grep -oE '"messages":\[.*' | head -c 600; echo

wait $MPID 2>/dev/null
echo
echo "===== metrics ====="
cat /tmp/bench_metrics.txt

echo
echo "==> deeper analysis:  bin/alp.sh   bin/slowlog.sh   bin/profile.sh"
