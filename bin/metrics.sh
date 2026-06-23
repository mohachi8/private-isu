#!/bin/bash
# Sample per-process CPU and per-core utilization while a benchmark runs, then
# print a one-screen summary. Start this in the BACKGROUND just before the
# benchmarker; it samples for DURATION seconds (default 30).
#
# HOW TO READ:
#   - Per-process %CPU: nginx + app + mysqld = the stack's real cost. Subtract
#     the *benchmarker* %CPU from the box total — what's left is the SUT cost.
#     (On the local co-located setup the benchmarker steals ~1 core, so the
#      stack looks artificially starved; the real env runs the bench elsewhere.)
#   - Per-core: high %idle  => the stack has spare CPU; the LOCAL score is
#     benchmarker-bound, not app-bound. High %soft (softirq) => network-interrupt
#     processing dominates (consider RPS/irq affinity). High %iowait => disk.
set -uo pipefail
DUR="${1:-30}"
PAT='app|nginx|mysqld|benchmarker'
TMP=$(mktemp -d)

echo "==> metrics: sampling ${DUR}s (run/await the benchmark now)"
free -m | awk '/Mem:/{printf "    mem  before: used=%sMB free=%sMB avail=%sMB\n",$3,$4,$7}'
ps -o rss= -C app 2>/dev/null | awk '{s+=$1} END{printf "    app RSS before: %.0fMB\n", s/1024}'
df -m / | awk 'NR==2{printf "    disk free before: %sMB\n",$4}'

pidstat -u 1 "$DUR" -C "$PAT" > "$TMP/pidstat.txt" 2>/dev/null &
P1=$!
mpstat -P ALL 1 "$DUR" > "$TMP/mpstat.txt" 2>/dev/null &
P2=$!
wait $P1 $P2 2>/dev/null

echo
echo "== per-process CPU% (avg over window; can exceed 100% across cores) =="
# pidstat Average line: $1=Average: $2=UID $3=PID $4=%usr $5=%system $7=%wait $8=%CPU ... $NF=Command
awk '/^Average:/ && $NF ~ /app|nginx|mysqld|benchmarker/ {cpu[$NF]+=$8; n[$NF]++}
     END{ for(c in cpu) printf "    %-12s %6.1f %%CPU  (%d proc/thread rows)\n", c, cpu[c], n[c] }' "$TMP/pidstat.txt" | sort

echo
echo "== per-core utilization (avg) =="
# mpstat Average: $2=CPU $3=%usr $5=%sys $6=%iowait $8=%soft $NF=%idle
awk '/^Average:/ && ($2 ~ /^[0-9]+$/ || $2=="all") {
       printf "    %-4s usr=%5.1f sys=%5.1f soft=%5.1f iowait=%5.1f idle=%5.1f\n",$2,$3,$5,$6,$8,$NF }' "$TMP/mpstat.txt"

echo
free -m | awk '/Mem:/{printf "    mem  after:  used=%sMB free=%sMB avail=%sMB\n",$3,$4,$7}'
ps -o rss= -C app 2>/dev/null | awk '{s+=$1} END{printf "    app RSS after:  %.0fMB\n", s/1024}'
df -m / | awk 'NR==2{printf "    disk free after:  %sMB\n",$4}'
rm -rf "$TMP"
