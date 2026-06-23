#!/bin/bash
# Prepare the box for an OFFICIAL scoring run (run this right before measuring).
#
# MOST IMPORTANT: reclaim disk headroom. Uploaded images pile up during a run and
# a full disk cascades into massive failures — this caused the real-env
# 485k -> 371k regression (fail 6701). So free-disk runs FIRST, every time.
# Then: free RAM by stopping services the app no longer uses, and turn off all
# measurement overhead (slow query log, nginx access log, pprof labels, gctrace).
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> reclaim disk headroom (free-disk.sh)"
"$SCRIPT_DIR/free-disk.sh" || true

echo "==> stop unused services to free RAM (memcached: cookie sessions; jaeger: tracing off)"
sudo systemctl stop memcached 2>/dev/null || true
sudo systemctl stop jaeger 2>/dev/null || true

echo "==> MySQL slow query log OFF"
sudo mysql -e "SET GLOBAL slow_query_log = 0;" || true

echo "==> nginx access_log OFF"
sudo sed -i -E 's|^(\s*)access_log .*|\1access_log off;|' /etc/nginx/nginx.conf
sudo nginx -t && sudo nginx -s reload

echo "==> pprof labels + GC trace OFF (remove drop-in)"
sudo rm -f /etc/systemd/system/isu-go.service.d/pprof.conf
sudo systemctl daemon-reload
sudo systemctl restart isu-go

# Wait until the app is ready: it rebuilds in-memory caches on startup, and
# measuring during warmup would fail requests.
echo -n "==> waiting for app readiness"
for i in $(seq 1 60); do
  if [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/ 2>/dev/null || true)" = "200" ]; then
    echo " ready"; break
  fi
  echo -n "."; sleep 0.5
done

echo "scoring mode READY: disk freed, RAM freed, logs/pprof/gctrace off. Run the bench now."
