#!/bin/bash
# Free root-FS space BEFORE an official benchmark.
#
# Why: uploaded images are written to disk during a run and only cleaned at the
# next /initialize. If the disk fills mid-run, MySQL writes and image writes
# fail, which cascades into thousands of benchmark failures (POST 500s, image
# 404s, and — via hung writes exhausting nginx upstream connections — failures
# on reads/static too). This reclaims headroom so a run won't fill the disk.
#
# Run on the APP SERVER (the box running MySQL + the Go app).
set -uo pipefail
echo "== disk before =="; df -h / | tail -1

# 1) MySQL binlog. Disabled binlog leaves orphaned files; remove them. If binlog
#    is still ENABLED it grows every run and is the main disk hog -> warn loudly.
LOGBIN=$(sudo mysql -N -e "SELECT @@log_bin" 2>/dev/null || echo "?")
if [ "$LOGBIN" = "0" ]; then
  echo "-> binlog disabled; removing orphaned binlog files"
  sudo find /var/lib/mysql -maxdepth 1 -name 'binlog.0*' -delete 2>/dev/null || true
  sudo rm -f /var/lib/mysql/binlog.index 2>/dev/null || true
elif [ "$LOGBIN" = "1" ]; then
  echo "!! WARNING: binlog is ENABLED (log_bin=1). It grows during every run and"
  echo "!! is likely what fills the disk. Apply infra/mysql config (disable-log-bin)"
  echo "!! via bin/apply-infra.sh and restart MySQL. Purging current binlogs now:"
  sudo mysql -e "PURGE BINARY LOGS BEFORE NOW();" 2>&1 | head -3 || true
fi

# 2) Truncate logs.
sudo truncate -s 0 /var/log/mysql/mysql-slow.log 2>/dev/null || true
sudo truncate -s 0 /var/log/nginx/access.log /var/log/nginx/error.log 2>/dev/null || true
sudo journalctl --vacuum-size=50M >/dev/null 2>&1 || true

# 3) Remove stray uploaded images (post id > 10000) from previous runs.
removed=$(find /home/isucon/private_isu/webapp/public/image -maxdepth 1 -type f 2>/dev/null \
  | awk -F'[/.]' '{ if (($(NF-1)+0) > 10000) print }' | tee /tmp/stray_imgs.txt | wc -l)
if [ "$removed" -gt 0 ]; then xargs -r rm -f < /tmp/stray_imgs.txt; fi
rm -f /tmp/stray_imgs.txt
echo "-> removed $removed stray uploaded image(s)"

echo "== disk after =="; df -h / | tail -1
echo "Headroom should comfortably exceed the uploads created during one run"
echo "(roughly ~1GB per 90s at high throughput)."
