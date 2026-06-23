#!/bin/bash
# Fast PDCA loop: build -> restart app -> short benchmark -> print score.
# Usage: bin/iter.sh [duration]   (default 25s; use 60s for an "official" run)
# Does NOT restart MySQL (keeps the buffer pool warm).
set -uo pipefail
export PATH="$PATH:/home/isucon/.local/go/bin"
DUR="${1:-25s}"

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

cd /home/isucon/private_isu/benchmarker
sudo truncate -s 0 /var/log/nginx/access.log /var/log/mysql/mysql-slow.log 2>/dev/null || true
./bin/benchmarker -t http://localhost -u ./userdata -benchmark-timeout "$DUR" 2>&1 \
  | grep -oE '"(pass|score|fail)":[a-z0-9]+' | tr '\n' ' '
echo
