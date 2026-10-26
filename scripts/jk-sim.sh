#!/bin/sh
# Giả lập node năng lượng khi chưa lắp pin: phát lại một kịch bản testdata/jk/*.txt lên MQTT (retain như ESPHome).
#   scripts/jk-sim.sh testdata/jk/pin-yeu.txt -h <broker> -p <cổng> -u node -P <mật khẩu>
# Tham số sau tên file chuyền thẳng cho mosquitto_pub. Máy không có mosquitto_pub thì dùng của container:
#   PUB="docker exec <container mosquitto> mosquitto_pub" scripts/jk-sim.sh testdata/jk/pin-yeu.txt -u node -P ...
# CHỈ phát vào broker của stack thử: số đo giả được giữ lại (retain) và bật cảnh báo thật trên dashboard.
# Luật có "for" (tới 2 phút) nên cảnh báo hiện sau vài phút.
set -eu
f=$1
shift
grep -v -e '^#' -e '^$' "$f" | while read -r topic payload; do
  ${PUB:-mosquitto_pub} -r -t "$topic" -m "$payload" "$@" </dev/null   # không để lệnh (vd docker exec -i) nuốt các dòng sau
done
