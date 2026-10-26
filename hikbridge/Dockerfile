# Dựng từ thư mục gốc repo (cần go.mod và gói isapi): docker compose dùng context "." + file này.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /hikbridge ./hikbridge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /hikbridge /hikbridge
ENTRYPOINT ["/hikbridge"]
