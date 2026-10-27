#!/bin/sh
# Khởi động Mosquitto (lệnh của service mosquitto trong docker-compose.yml).
# .env có HUBD_MQTT_ADMIN_PASS (scripts/init-secrets.sh): bật thêm plugin dynamic security để hubd tạo/khoá tài khoản
# node ESP32 từ dashboard (nodes.go). Chưa có: chạy y như trước, chỉ mosquitto.conf + password_file.
#
# - Lần đầu tạo file cấu hình plugin với đúng một tài khoản quản trị (chỉ có quyền quản lý tài khoản). Không để plugin
#   tự sinh file: bản tự sinh có sẵn "democlient" toàn quyền.
# - Có plugin mà không có acl_file thì tài khoản trong passwd (hubd, tuya, node...) bị chặn mọi topic. acl_file một dòng
#   "pattern readwrite #" giữ nguyên quyền cũ của chúng; tài khoản trong plugin do plugin xét trước (role riêng từng node).
# - Plugin xét trước passwd: tài khoản plugin trùng tên sẽ thay tài khoản passwd. hubd chỉ tạo tên node-<khu>.
set -eu
conf=/mosquitto/config/mosquitto.conf
if [ -n "${MQTT_ADMIN_PASS:-}" ]; then
  umask 077
  f=/mosquitto/data/dynamic-security.json
  if [ ! -f "$f" ]; then
    mosquitto_ctrl dynsec init "$f" "${MQTT_ADMIN_USER:-hubd-admin}" "$MQTT_ADMIN_PASS" >/dev/null
  fi
  echo 'pattern readwrite #' > /tmp/acl
  { cat "$conf"; printf '\nplugin /usr/lib/mosquitto_dynamic_security.so\nplugin_opt_config_file %s\nacl_file /tmp/acl\n' "$f"; } > /tmp/mosquitto.conf
  chown mosquitto:mosquitto "$f" /tmp/acl /tmp/mosquitto.conf
  conf=/tmp/mosquitto.conf
fi
exec mosquitto -c "$conf"
