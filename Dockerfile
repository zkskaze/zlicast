FROM golang:1.26-bookworm AS backend
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends libsqlite3-dev && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY web/build ./web/build
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/zlicast .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg espeak-ng libsqlite3-0 ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/zlicast
COPY --from=backend /out/zlicast /opt/zlicast/zlicast
ENV TZ=Europe/Belgrade ZLICAST_ADDR=:8080 ZLICAST_DATA=/data
EXPOSE 8080
ENTRYPOINT ["/opt/zlicast/zlicast"]


