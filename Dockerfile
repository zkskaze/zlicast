FROM denoland/deno:bin-2.9.8 AS youtube-runtime

FROM golang:1.26-bookworm AS backend
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends curl libsqlite3-dev && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY web/build ./web/build
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/zlicast .
ARG TARGETARCH
ARG YT_DLP_VERSION=2026.11.23
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) asset=yt-dlp_linux ;; \
      arm64) asset=yt-dlp_linux_aarch64 ;; \
      *) exit 1 ;; \
    esac; \
    release="https://github.com/yt-dlp/yt-dlp/releases/download/$YT_DLP_VERSION"; \
    cd /out; \
    curl -fsSLO "$release/$asset"; \
    curl -fsSLO "$release/SHA2-256SUMS"; \
    grep "  $asset$" SHA2-256SUMS | sha256sum -c -; \
    mv "$asset" yt-dlp; \
    chmod +x yt-dlp

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng libsqlite3-0 ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/zlicast
COPY --from=backend /out/zlicast /opt/zlicast/zlicast
COPY --from=backend /out/yt-dlp /usr/local/bin/yt-dlp
COPY --from=youtube-runtime /deno /usr/local/bin/deno
RUN yt-dlp --version && deno --version
ENV TZ=Europe/Belgrade ZLICAST_PORT=8080 ZLICAST_DATA=/data
EXPOSE 8080
ENTRYPOINT ["/opt/zlicast/zlicast"]


