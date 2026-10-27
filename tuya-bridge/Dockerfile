FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
# Quét QR trên dashboard dò IP thiết bị bằng raw socket (đọc broadcast dù ufw chặn): cấp CAP_NET_RAW cho riêng
# trình Python để chạy được bằng user nobody (Docker mặc định có NET_RAW trong tập quyền của container).
RUN apt-get update && apt-get install -y --no-install-recommends libcap2-bin && rm -rf /var/lib/apt/lists/* \
 && setcap cap_net_raw+ep "$(readlink -f "$(command -v python3)")"
COPY bridge.py gen_config.py qrsetup.py ./
RUN mkdir /state && chown nobody:nogroup /state   # volume tuya-state: kWh cộng dồn + tuya.yaml sinh cho hubd
USER nobody
CMD ["python", "-u", "bridge.py"]
