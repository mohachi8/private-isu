#!/bin/bash
# Deploy infra config files from the repo to their system locations,
# then reload/restart the affected services. Run after editing infra/.
set -euo pipefail
REPO=/home/isucon/private_isu

echo "==> nginx config"
sudo cp "$REPO/infra/nginx/nginx.conf" /etc/nginx/nginx.conf
sudo cp "$REPO/infra/nginx/sites-available/isucon.conf" /etc/nginx/sites-available/isucon.conf
sudo ln -sf /etc/nginx/sites-available/isucon.conf /etc/nginx/sites-enabled/isucon.conf
sudo nginx -t
sudo systemctl reload nginx

echo "==> mysql config"
sudo cp "$REPO/infra/mysql/conf.d/zz-isucon.cnf" /etc/mysql/conf.d/zz-isucon.cnf
sudo systemctl restart mysql

echo "==> done"
