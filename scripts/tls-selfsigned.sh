#!/bin/sh
# HTTPS cho hubd (cổng 8443): Safari trên iPad chỉ cho dùng micro (nút nói của trợ lý) trên trang HTTPS.
# CA riêng của nhà nằm ở tls-ca/ (KHÔNG gắn vào container nào); chứng chỉ cho IP hub ở tls/ (hubd đọc).
# CA chỉ ký được IP nội bộ và tên *.home.arpa: lộ khoá CA cũng không giả được trang ngoài internet.
# Cả hai thư mục không commit. Chạy lại để gia hạn chứng chỉ (giữ CA cũ).
# iPad tin CA một lần: AirDrop tls-ca/ca.crt → Cài đặt → Đã tải về hồ sơ → Cài đặt;
# rồi Cài đặt → Cài đặt chung → Giới thiệu → Cài đặt tin cậy chứng nhận → bật "iNIX Home CA".
set -eu
cd "$(dirname "$0")/.."
IP=${1:-192.168.1.254}
umask 077
mkdir -p tls tls-ca
[ -f tls-ca/ca.key ] || openssl req -x509 -newkey rsa:2048 -nodes -keyout tls-ca/ca.key -out tls-ca/ca.crt -days 3650 \
  -subj "/CN=iNIX Home CA" \
  -addext "nameConstraints=critical,permitted;IP:10.0.0.0/255.0.0.0,permitted;IP:172.16.0.0/255.240.0.0,permitted;IP:192.168.0.0/255.255.0.0,permitted;DNS:home.arpa"
# CN không được giống tên miền/IP: OpenSSL đem CN đó so với giới hạn tên của CA. Tên thật nằm ở SAN.
openssl req -newkey rsa:2048 -nodes -keyout tls/hub.key -out tls-ca/hub.csr -subj "/CN=iNIX Home hub"
printf 'subjectAltName=IP:%s\nextendedKeyUsage=serverAuth\n' "$IP" > tls-ca/ext.cnf
# iOS chỉ nhận chứng chỉ máy chủ hạn tối đa 825 ngày.
openssl x509 -req -in tls-ca/hub.csr -CA tls-ca/ca.crt -CAkey tls-ca/ca.key -CAcreateserial -out tls/hub.crt -days 825 -extfile tls-ca/ext.cnf
rm -f tls-ca/hub.csr tls-ca/ext.cnf
chmod 644 tls-ca/ca.crt tls/hub.crt
echo "Xong. Thêm vào .env: HUBD_TLS_CERT=/tls/hub.crt và HUBD_TLS_KEY=/tls/hub.key, rồi docker compose up -d hubd"
