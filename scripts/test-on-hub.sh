#!/usr/bin/env bash
# Chạy toàn bộ test trên máy Ubuntu thay vì máy Mac (Mac hết RAM khi chạy go test / Docker / venv).
#   scripts/test-on-hub.sh            # thư mục thử đặt theo tên nhánh hiện tại
#   scripts/test-on-hub.sh tên-khác   # đặt tên thư mục thử khác
# Đẩy cây làm việc hiện tại (file đã commit + chưa commit, bỏ file bị gitignore) lên /root/hubd-ci/<tên>,
# chạy test Go (cùng image golang với Dockerfile) và test Python (python:3.12-slim) trong container.
# Không đụng stack đang chạy ở /root/hubd. Bộ nhớ đệm module/pip nằm ở volume hubd-ci-* nên lần sau nhanh.
set -euo pipefail
HUB=${HUB:-root@192.168.1.254}
cd "$(git rev-parse --show-toplevel)"
name=${1:-$(git branch --show-current)}
name=${name//[^A-Za-z0-9._-]/-}
dir=/root/hubd-ci/$name
goimg=$(awk '/^FROM golang/ {print $2; exit}' Dockerfile)

echo "== đẩy cây làm việc lên $HUB:$dir"
git ls-files -z --cached --others --exclude-standard | COPYFILE_DISABLE=1 tar --no-xattrs --no-mac-metadata --null -T - -czf - |
  ssh -o ConnectTimeout=8 "$HUB" "rm -rf '$dir' && mkdir -p '$dir' && tar -xzf - -C '$dir'"

# Test Python: mỗi thư mục tự khai báo thư viện cần cho test (requirements-test.txt, không có thì requirements.txt).
ssh -o ConnectTimeout=8 "$HUB" bash -s -- "$dir" "$goimg" <<'REMOTE'
set -uo pipefail
dir=$1 goimg=$2 fail=0
echo "== go test ($goimg)"
docker run --rm -v "$dir":/src -w /src -v hubd-ci-gomod:/go/pkg/mod -v hubd-ci-gocache:/root/.cache/go-build \
  "$goimg" sh -c 'go vet ./... && go test ./...' || fail=1
echo "== test Python"
docker run --rm -v "$dir":/src -w /src -v hubd-ci-pip:/root/.cache/pip python:3.12-slim sh -c '
  rc=0
  for d in tuya-bridge scripts voiced; do
    [ -d "$d" ] && ls "$d"/test_*.py >/dev/null 2>&1 || continue
    req="$d/requirements-test.txt"; [ -f "$req" ] || req="$d/requirements.txt"
    echo "-- $d"
    if [ -f "$req" ]; then pip install -q --disable-pip-version-check --root-user-action=ignore -r "$req" pyyaml || { rc=1; continue; }; fi
    (cd "$d" && python -m unittest discover -q) || rc=1
  done
  exit $rc' || fail=1
[ $fail = 0 ] && echo "== TẤT CẢ ĐẠT" || echo "== CÓ TEST HỎNG"
exit $fail
REMOTE
