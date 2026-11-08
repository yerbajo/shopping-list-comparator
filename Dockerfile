# syntax=docker/dockerfile:1
# Two stages: the binary is built with the toolchain, the runtime keeps only the binary and
# the CA certificates. No cgo, no shell in the build output, and the list lives in a volume
# that belongs to the unprivileged user the app runs as.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/web ./cmd/web

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 lista \
 && mkdir -p /data \
 && chown lista:lista /data
WORKDIR /app
COPY --from=build /out/web /app/web
USER lista
VOLUME ["/data"]
EXPOSE 8100
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8100/api/state >/dev/null || exit 1
# The engine URL is a flag, not an environment variable: what a deployment must decide is
# visible in the command it runs. Override the whole command to point at your multisuper.
ENTRYPOINT ["/app/web"]
CMD ["-addr", "0.0.0.0:8100", "-multisuper", "http://host.docker.internal:8099", "-data", "/data/list.json"]
