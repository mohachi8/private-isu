#!/bin/bash
# Summarize the nginx access log (LTSV) with alp, sorted by total response time.
# Endpoints with dynamic IDs are grouped via -m.
set -uo pipefail
sudo alp ltsv --file /var/log/nginx/access.log \
  --sort sum -r \
  -m '/posts/\d+,/image/\d+\.\w+,/@\w+' \
  -o count,method,uri,min,avg,max,sum,p99
