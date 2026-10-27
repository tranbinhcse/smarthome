# Spotify phát qua hub (spotifyhub.go, mục 18.3): librespot 0.8.0 dựng tĩnh (musl, rustls kèm chứng chỉ gốc), không mDNS,
# không ALSA (chỉ ghi PCM vào FIFO cho hubd). Lần đầu mất khoảng 3 phút; Docker giữ lớp này cho các lần sau.
FROM rust:1-alpine AS librespot
RUN apk add --no-cache musl-dev binutils
RUN cargo install librespot --version 0.8.0 --locked --no-default-features --features rustls-tls-webpki-roots --root /out \
 && strip /out/bin/librespot

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /hubd .

FROM gcr.io/distroless/static-debian12
COPY --from=librespot /out/bin/librespot /usr/local/bin/librespot
COPY --from=build /hubd /hubd
EXPOSE 8080
ENTRYPOINT ["/hubd"]
