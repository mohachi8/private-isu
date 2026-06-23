#!/bin/bash
# Summarize the MySQL slow query log. Uses pt-query-digest if available,
# otherwise falls back to mysqldumpslow.
set -uo pipefail
LOG=/var/log/mysql/mysql-slow.log
if command -v pt-query-digest >/dev/null 2>&1; then
  sudo pt-query-digest "$LOG" | head -80
else
  echo "(pt-query-digest not installed; using mysqldumpslow -s t)"
  sudo mysqldumpslow -s t "$LOG" | head -60
fi
