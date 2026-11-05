FROM golang:1.27.3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /out && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ratelimitx ./cmd/server

FROM alpine:3.22

RUN addgroup -S ratelimitx && adduser -S -G ratelimitx ratelimitx
COPY --from=build /out/ratelimitx /usr/local/bin/ratelimitx

USER ratelimitx
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ratelimitx"]
