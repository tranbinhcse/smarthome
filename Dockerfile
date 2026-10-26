FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /hubd .

FROM gcr.io/distroless/static-debian12
COPY --from=build /hubd /hubd
EXPOSE 8080
ENTRYPOINT ["/hubd"]
