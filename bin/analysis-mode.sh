#!/bin/bash
# Re-enable measurement (slow query log, nginx LTSV access log, pprof labels).
# Use while investigating; switch to bin/scoring-mode.sh for the official run.
set -uo pipefail
echo "==> MySQL slow query log ON (long_query_time=0)"
sudo mysql -e "SET GLOBAL slow_query_log = 1;"
echo "==> nginx access_log ON (ltsv)"
sudo sed -i -E 's|^(\s*)access_log .*|\1access_log /var/log/nginx/access.log ltsv;|' /etc/nginx/nginx.conf
sudo nginx -t && sudo nginx -s reload
echo "==> pprof labels ON + GC trace ON (drop-in)"
sudo mkdir -p /etc/systemd/system/isu-go.service.d
printf '[Service]\nEnvironment=PPROF_LABELS=1\nEnvironment=GODEBUG=gctrace=1\n' \
  | sudo tee /etc/systemd/system/isu-go.service.d/pprof.conf >/dev/null
sudo systemctl daemon-reload
sudo systemctl restart isu-go
echo "analysis mode ON (slow log + access log + pprof labels + gctrace)"
echo "  GC trace: sudo journalctl -u isu-go -f | grep '^gc '"
