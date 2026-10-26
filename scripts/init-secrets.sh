#!/bin/sh
# Sinh .env (PIN + mật khẩu MQTT) và mosquitto/passwd. Chạy một lần; có sẵn .env thì không ghi đè.
# Không in bí mật ra màn hình — đọc từ .env khi cần.
set -eu
cd "$(dirname "$0")/.."
[ -f .env ] && { echo ".env đã có, giữ nguyên (xoá đi nếu muốn sinh lại)"; exit 0; }
rnd() { head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 20; }
PIN=$(head -c 4 /dev/urandom | od -An -tu4 | tr -dc 0-9 | head -c 6)
HUBD_PASS=$(rnd); TUYA_PASS=$(rnd); NODE_PASS=$(rnd)
umask 077
cat > .env <<ENV
HUBD_PIN=$PIN
HUBD_MQTT_USER=hubd
HUBD_MQTT_PASS=$HUBD_PASS
TUYA_MQTT_USER=tuya
TUYA_MQTT_PASS=$TUYA_PASS
# Tài khoản cho node ESPHome (dùng sau):
NODE_MQTT_USER=node
NODE_MQTT_PASS=$NODE_PASS
ENV
: > mosquitto/passwd
for u in hubd:$HUBD_PASS tuya:$TUYA_PASS node:$NODE_PASS; do
  docker run --rm -v "$PWD/mosquitto:/m" eclipse-mosquitto:2 mosquitto_passwd -b /m/passwd "${u%%:*}" "${u#*:}"
done
chmod 644 mosquitto/passwd   # container mosquitto đọc bằng user khác
echo "Đã tạo .env và mosquitto/passwd. Xem PIN: grep HUBD_PIN .env"
