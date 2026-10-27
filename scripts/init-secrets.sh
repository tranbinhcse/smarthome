#!/bin/sh
# Sinh .env (PIN + mật khẩu MQTT) và mosquitto/passwd. Chạy một lần; có sẵn .env thì không ghi đè.
# Không in bí mật ra màn hình — đọc từ .env khi cần.
set -eu
cd "$(dirname "$0")/.."
rnd() { head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 20; }
mqtt_user() { docker run --rm -v "$PWD/mosquitto:/m" eclipse-mosquitto:2 mosquitto_passwd -b /m/passwd "$1" "$2"; }
if [ -f .env ] && ! grep -q '^HUBD_CAMERA_TOKEN=.' .env; then
  # .env có từ trước khi nhập đầu ghi trên dashboard: thêm token camera (hikbridge đọc cấu hình qua hubd)
  umask 077
  { grep -v '^HUBD_CAMERA_TOKEN=' .env || true; } > .env.tmp && mv .env.tmp .env
  printf 'HUBD_CAMERA_TOKEN=%s\n' "$(rnd)$(rnd)" >> .env
  echo "Đã thêm HUBD_CAMERA_TOKEN vào .env"
fi
# Tài khoản quản trị plugin dynamic security: hubd tạo/khoá tài khoản node ESP32 từ dashboard (mosquitto/start.sh).
admin_env() { printf '# Quản trị tài khoản node ESP32 (mosquitto/start.sh tạo khi khởi động Mosquitto):\nHUBD_MQTT_ADMIN_USER=hubd-admin\nHUBD_MQTT_ADMIN_PASS=%s\n' "$(rnd)$(rnd)"; }
if [ -f .env ]; then
  # .env có từ trước: chỉ thêm khoá còn thiếu, không đổi khoá cũ.
  umask 077
  if ! grep -q '^HUBD_MQTT_ADMIN_PASS=.' .env; then
    { grep -v -E '^HUBD_MQTT_ADMIN_(USER|PASS)=' .env || true; } > .env.tmp && mv .env.tmp .env
    admin_env >> .env
    echo "Đã thêm tài khoản quản trị node vào .env. Bật: docker compose up -d mosquitto hubd (Mosquitto khởi động lại vài giây)"
  fi
  grep -q '^VOICED_MQTT_PASS=.' .env && { echo ".env đã đủ khoá, giữ nguyên (xoá đi nếu muốn sinh lại)"; exit 0; }
  VOICED_PASS=$(rnd)
  mqtt_user voiced "$VOICED_PASS"   # tạo tài khoản trước: lỗi thì .env chưa đổi, chạy lại được
  chmod 644 mosquitto/passwd
  # bỏ dòng cũ/trống (vd chép từ .env.example) rồi ghi lại một lần
  { grep -v -E '^(HUBD_ASSISTANT_TOKEN|VOICED_MQTT_USER|VOICED_MQTT_PASS)=' .env || true; } > .env.tmp && mv .env.tmp .env
  printf 'HUBD_ASSISTANT_TOKEN=%s\nVOICED_MQTT_USER=voiced\nVOICED_MQTT_PASS=%s\n' "$(rnd)$(rnd)" "$VOICED_PASS" >> .env
  grep -q '^ANTHROPIC_API_KEY=' .env || printf '# Khoá Claude cho câu tự do (trống = chỉ lệnh cố định):\nANTHROPIC_API_KEY=\n' >> .env
  echo "Đã thêm token trợ lý và tài khoản MQTT voiced vào .env. Nạp tài khoản mới: docker compose restart mosquitto"
  exit 0
fi
PIN=$(head -c 4 /dev/urandom | od -An -tu4 | tr -dc 0-9 | head -c 6)
HUBD_PASS=$(rnd); TUYA_PASS=$(rnd); NODE_PASS=$(rnd); HIK_PASS=$(rnd); VOICED_PASS=$(rnd)
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
# Camera Hikvision: bỏ # ở COMPOSE_PROFILES rồi nhập đầu ghi trên dashboard, Cài đặt → Camera (README mục Camera)
#COMPOSE_PROFILES=camera
HUBD_CAMERA_TOKEN=$(rnd)$(rnd)
NVR_HOST=
NVR_USER=
NVR_PASS=
HIK_MQTT_USER=hik
HIK_MQTT_PASS=$HIK_PASS
# Trợ lý giọng nói (voiced):
HUBD_ASSISTANT_TOKEN=$(rnd)$(rnd)
VOICED_MQTT_USER=voiced
VOICED_MQTT_PASS=$VOICED_PASS
# Khoá Claude cho câu tự do (trống = chỉ lệnh cố định):
ANTHROPIC_API_KEY=
$(admin_env)
ENV
: > mosquitto/passwd
for u in hubd:$HUBD_PASS tuya:$TUYA_PASS node:$NODE_PASS hik:$HIK_PASS voiced:$VOICED_PASS; do
  mqtt_user "${u%%:*}" "${u#*:}"
done
chmod 644 mosquitto/passwd   # container mosquitto đọc bằng user khác
echo "Đã tạo .env và mosquitto/passwd. Xem PIN: grep HUBD_PIN .env"
