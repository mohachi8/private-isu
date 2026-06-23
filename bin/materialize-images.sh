#!/bin/bash
# Materialize every seeded image (post id <= 10000) to disk so nginx can serve
# them statically and the app never needs to read imgdata from the DB.
# Idempotent: files already present are just re-served by nginx (cheap).
set -uo pipefail
TARGET="${1:-http://localhost}"

echo "==> listing image ids/mime from DB"
# Output: "<id> <ext>" per row
sudo mysql -N isuconp -e "SELECT id, mime FROM posts" 2>/dev/null | \
awk '{
  ext = "";
  if ($2 == "image/jpeg") ext = "jpg";
  else if ($2 == "image/png") ext = "png";
  else if ($2 == "image/gif") ext = "gif";
  if (ext != "") print "/image/" $1 "." ext;
}' > /tmp/image_urls.txt

n=$(wc -l < /tmp/image_urls.txt)
echo "==> requesting $n images (parallel) to trigger materialization"
# -P 16 parallel; nginx serves existing files from disk, misses hit the app
xargs -P 16 -I{} curl -s -o /dev/null "$TARGET{}" < /tmp/image_urls.txt

echo "==> done. files on disk: $(ls /home/isucon/private_isu/webapp/public/image/ | wc -l)"
rm -f /tmp/image_urls.txt
