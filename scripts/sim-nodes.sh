#!/bin/sh
# Giả lập các node ESP32 chưa lắp: gửi một giá trị mẫu cho mọi entity node-* trong config.yaml,
# hoặc một topic tuỳ chọn (thử luật: CO₂ cao, mưa, rò nước).
#
#   PUB="mosquitto_pub -h 127.0.0.1 -p 1883 -u node -P <NODE_MQTT_PASS>" scripts/sim-nodes.sh
#   PUB="..." scripts/sim-nodes.sh node-cafe/sensor/co2/state 1500
#
# PUB bắt buộc khai báo: giá trị giả ghi vào lịch sử SQLite của hubd, đừng trỏ vào trung tâm đang chạy thật.
set -eu
cd "$(dirname "$0")/.."
: "${PUB:?đặt PUB=\"mosquitto_pub -h <broker> -u <user> -P <mật khẩu>\" (xem đầu file)}"

if [ $# -eq 2 ]; then
  $PUB -t "$1" -m "$2"
  echo "$1 = $2"
  exit 0
fi

n=0
for t in $(grep -o '[ {]topic: node-[^ ,}]*' config.yaml | sed 's/^. *topic: //'); do
  s=${t%/state}; s=${s##*/}
  case "$t" in node-gieng-troi/*) out=1 ;; *) out=0 ;; esac
  case "$s" in
    nhiet_do) [ $out = 1 ] && v=31.2 || v=27.5 ;;
    do_am) [ $out = 1 ] && v=71 || v=62 ;;
    co2) v=750 ;;
    pm25) v=14 ;;
    voc) v=110 ;;
    lux) [ $out = 1 ] && v=18400 || v=320 ;;
    co_nguoi) v=1 ;;
    mua) v=0 ;;
    tds_nguon) v=182 ;;
    tds_sau_loc) v=11 ;;
    ti_le_loai_bo) v=94.0 ;;
    da_loc_hom_nay) v=23.5 ;;
    loi_loc_con) v=80 ;;
    ro_nuoc) v=0 ;;
    *) echo "bỏ qua $t: chưa có giá trị mẫu cho '$s'" >&2; continue ;;
  esac
  $PUB -t "$t" -m "$v"
  n=$((n + 1))
done
echo "đã gửi $n giá trị mẫu"
