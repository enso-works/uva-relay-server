FROM golang:1.23-alpine AS builder

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
ENTRYPOINT ["uvame-relay"]
