FROM golang:1.26-alpine AS build

WORKDIR /src
ARG VERSION=local
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
# Static binary: the runtime image has no libc to link against.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.buildVersion=${VERSION}" -o /out/pixelhub ./cmd/pixelhub \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pixelbridge ./cmd/pixelbridge

FROM alpine:3.21

WORKDIR /app
RUN apk add --no-cache tzdata pipewire-tools \
    && adduser -D -H -u 10001 pixelhub \
    && install -d -o pixelhub -g pixelhub -m 700 /data
COPY --from=build /out/pixelhub /out/pixelbridge /usr/local/bin/

# state.json, sprites/ and ambient/ live here.
VOLUME /data

ENV PIXELHUB_LOG_JSON=1 \
    PIXELHUB_DATA_DIR=/data \
    PIXELHUB_PORT=8000

EXPOSE 8000
USER pixelhub

HEALTHCHECK --interval=60s --timeout=10s --start-period=15s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PIXELHUB_PORT}/healthz" >/dev/null || exit 1

ENTRYPOINT ["pixelhub"]
