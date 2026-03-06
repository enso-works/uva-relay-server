FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o uvame-relay ./cmd/relay

FROM alpine:3.21
RUN adduser -D -h /data relay
COPY --from=builder /build/uvame-relay /usr/local/bin/uvame-relay
USER relay
WORKDIR /data
VOLUME ["/data"]
EXPOSE 4400
ENV RELAY_DATA_DIR=/data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:4400/ready >/dev/null || exit 1
ENTRYPOINT ["uvame-relay"]
