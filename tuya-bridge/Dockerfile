FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY bridge.py .
RUN mkdir /state && chown nobody:nogroup /state   # tổng kWh cộng dồn (volume tuya-state)
USER nobody
CMD ["python", "-u", "bridge.py"]
