#!/bin/bash
# Optimize for the official scoring run: turn OFF the measurement overhead
# (slow query log, nginx access log, pprof endpoint labels). ~+2% vs analysis.
set -uo pipefail
echo "==> MySQL slow query log OFF"
sudo mysql -e "SET GLOBAL slow_query_log = 0;"
echo "==> nginx access_log OFF"
sudo sed -i -E 's|^(\s*)access_log .*|\1access_log off;|' /etc/nginx/nginx.conf
sudo nginx -t && sudo nginx -s reload
echo "==> pprof labels OFF (remove drop-in)"
sudo rm -f /etc/systemd/system/isu-go.service.d/pprof.conf
sudo systemctl daemon-reload
sudo systemctl restart isu-go
echo "scoring mode ON (logs off, pprof labels off)"
