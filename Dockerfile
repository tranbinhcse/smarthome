# Spotify phát qua hub (spotifyhub.go, mục 18.3): hubd chạy Soloist và PulseAudio như tiến trình con. Soloist không nằm trong
# image (Spotify không cho phân phối lại): hubd tải lúc chạy vào /db/soloist. Soloist chỉ nhận khoá API qua -k (ai cũng đọc
# được ở /proc/<pid>/cmdline): soloist-key.so đổi tham số giữ chỗ thành khoá trong biến môi trường (soloist/key.c).
FROM debian:trixie-slim AS soloistkey
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
COPY soloist/key.c /src/key.c
RUN gcc -O2 -Wall -shared -fPIC -o /soloist-key.so /src/key.c \
 && test "$(SOLOIST_API_KEY=ok LD_PRELOAD=/soloist-key.so /bin/echo @SOLOIST_API_KEY)" = ok

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /hubd .

# Soloist cần glibc ≥ 2.38 (trixie), libatomic1, chứng chỉ gốc, và PulseAudio (hoặc PipeWire) để xuất âm thanh.
# tini làm PID 1: crashpad-handler của Soloist tách khỏi cha, thoát thì cần init dọn (không thì thành zombie dưới hubd).
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libatomic1 pulseaudio tini \
 && rm -rf /var/lib/apt/lists/*
COPY --from=soloistkey /soloist-key.so /usr/local/lib/soloist-key.so
COPY --from=build /hubd /hubd
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--", "/hubd"]
