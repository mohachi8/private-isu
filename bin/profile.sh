#!/bin/bash
# Capture a CPU profile (and optionally heap) from the running app while a
# benchmark is in progress, print the top functions, and save artifacts.
# Usage: bin/profile.sh [SECONDS] [--heap]
# Tip: run `bin/analysis-mode.sh` first so pprof endpoint labels are on, then
#      start a bench in another terminal and run this.
set -uo pipefail
export PATH="$PATH:/home/isucon/.local/go/bin"
SEC="${1:-20}"
APP=/home/isucon/private_isu/webapp/golang/app
OUT=/tmp/profile
mkdir -p "$OUT"

echo "==> CPU profile for ${SEC}s (make sure a bench is running)"
curl -s -o "$OUT/cpu.pprof" "http://localhost:6060/debug/pprof/profile?seconds=${SEC}"
echo "== top (flat) =="
go tool pprof -top -nodecount=30 "$APP" "$OUT/cpu.pprof" 2>/dev/null | sed -n '/flat /,$p' | head -31
echo "== per-endpoint (needs PPROF_LABELS=1 / analysis-mode) =="
go tool pprof -tags "$OUT/cpu.pprof" 2>/dev/null | sed -n '/endpoint/,/^$/p' | head -12

if command -v dot >/dev/null 2>&1; then
  go tool pprof -svg -output="$OUT/cpu.svg" "$APP" "$OUT/cpu.pprof" 2>/dev/null && echo "flamegraph: $OUT/cpu.svg"
else
  echo "(graphviz/dot not installed -> no static svg. For an interactive flame graph run:"
  echo "   go tool pprof -http=:8081 $APP $OUT/cpu.pprof   )"
fi

if [ "${2:-}" = "--heap" ]; then
  echo "== heap (inuse_space) =="
  curl -s -o "$OUT/heap.pprof" "http://localhost:6060/debug/pprof/heap"
  go tool pprof -top -sample_index=inuse_space -nodecount=15 "$APP" "$OUT/heap.pprof" 2>/dev/null | sed -n '/flat /,$p' | head -16
  echo "== allocs (alloc_space) =="
  curl -s -o "$OUT/allocs.pprof" "http://localhost:6060/debug/pprof/allocs"
  go tool pprof -top -alloc_space -nodecount=15 "$APP" "$OUT/allocs.pprof" 2>/dev/null | sed -n '/flat /,$p' | head -16
fi
echo "==> artifacts in $OUT/"
