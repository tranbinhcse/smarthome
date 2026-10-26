FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
# Mô hình STT/TTS tải lúc build, nằm sẵn trong image: khởi động và chạy không cần internet.
COPY fetch_models.py .
RUN python fetch_models.py && mkdir /cache && chown nobody /cache
COPY voiced.py .
ENV HF_HUB_OFFLINE=1 HF_HOME=/tmp/hf NUMBA_CACHE_DIR=/tmp/numba
USER nobody
EXPOSE 8090
CMD ["python", "-u", "voiced.py"]
