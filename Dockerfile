# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:24-alpine3.24 AS web-builder

ARG PNPM_VERSION=11.3.0

WORKDIR /src/web

RUN corepack enable \
    && corepack prepare "pnpm@${PNPM_VERSION}" --activate

COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/pnpm/store \
    pnpm config set store-dir /pnpm/store \
    && pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm build


FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine3.24 AS go-builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web-builder /src/internal/webui/dist/ ./internal/webui/dist/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build \
      -buildvcs=false \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
      -o /out/rssdownload \
      ./cmd/rssdownload


FROM alpine:3.24.1

LABEL org.opencontainers.image.title="RSSDownload" \
      org.opencontainers.image.description="A self-hosted RSS downloader for Mikan backed by aria2" \
      org.opencontainers.image.source="https://github.com/Rehtt/rssdownload" \
      org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 rssdownload \
    && adduser -S -D -H -u 10001 -G rssdownload rssdownload \
    && install -d -o rssdownload -g rssdownload -m 0700 /data

COPY --from=go-builder --chown=rssdownload:rssdownload \
    /out/rssdownload /usr/local/bin/rssdownload

USER rssdownload:rssdownload
WORKDIR /data

ENV RSSDOWNLOAD_LISTEN="0.0.0.0:8080" \
    RSSDOWNLOAD_DATA_DIR="/data"

EXPOSE 8080
VOLUME ["/data"]
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/api/v1/health || exit 1

ENTRYPOINT ["/usr/local/bin/rssdownload"]
