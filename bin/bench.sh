#!/bin/bash
# Rotate logs, run the benchmark, then print alp + slow-query summaries.
set -uo pipefail

TARGET="${1:-http://localhost}"
BENCH_DIR=/home/isucon/private_isu/benchmarker

# --- rotate logs so analysis only covers this run ---
echo "==> rotating logs"
sudo truncate -s 0 /var/log/nginx/access.log 2>/dev/null || true
sudo truncate -s 0 /var/log/mysql/mysql-slow.log 2>/dev/null || true

echo "==> running benchmark against $TARGET"
cd "$BENCH_DIR"
./bin/benchmarker -t "$TARGET" -u ./userdata
echo

echo "==> done. Analyze with:  bin/alp.sh   and   bin/slowlog.sh"
