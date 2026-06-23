#!/bin/bash
# Build the Go app and restart the service. Use after editing webapp/golang/*.go.
set -euo pipefail
export PATH="$PATH:/home/isucon/.local/go/bin"
cd /home/isucon/private_isu/webapp/golang
echo "==> building..."
go build -o app
echo "==> restarting isu-go..."
sudo systemctl restart isu-go
sleep 1
systemctl is-active isu-go
curl -s -o /dev/null -w "health: HTTP %{http_code} %{time_total}s\n" http://localhost/
