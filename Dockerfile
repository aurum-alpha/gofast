# syntax=docker/dockerfile:1
#
# LOCAL / DEV — build UI + Go from source (docker compose build).
# Production/GHCR uses Dockerfile.prod (CI binaries only). Keep image pins in sync:
#   node:26-bookworm
#   golang:1.26.6-bookworm
#   debian:bookworm-slim (fastgen + fastproxy — ships ffmpeg)
#   busybox:1.36.1-musl (legacy note; fastproxy HEALTHCHECK uses wget from apt)

FROM node:26-bookworm AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
# corepack activates the pnpm version pinned by packageManager in package.json.
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN mkdir -p /src/internal/ui && pnpm run build

# golang:1.26.6-bookworm
FROM golang@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/ui/dist/ internal/ui/dist/
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=0
# Optional identity for local compose builds (CI injects via ldflags on binaries).
ARG BUILD_NUMBER=local
ARG GIT_COMMIT=
ARG BUILD_TIME=
RUN LDFLAGS="-s -w \
      -X github.com/j27-aurum/gofast/internal/version.Build=${BUILD_NUMBER} \
      -X github.com/j27-aurum/gofast/internal/version.Commit=${GIT_COMMIT} \
      -X github.com/j27-aurum/gofast/internal/version.BuiltAt=${BUILD_TIME}" \
    && go build -buildvcs=false -trimpath -ldflags="$LDFLAGS" -o /out/fastgen ./cmd/fastgen \
    && go build -buildvcs=false -trimpath -ldflags="$LDFLAGS" -o /out/fastproxy ./cmd/fastproxy

# fastgen needs ffprobe (Health L2). Use slim Debian + ffmpeg rather than
# distroless/static (dynamically linked ffprobe will not run there).
FROM debian:bookworm-slim AS fastgen
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg wget \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --uid 65532 --user-group --no-create-home --shell /usr/sbin/nologin nonroot
COPY --from=build /out/fastgen /fastgen
USER nonroot:nonroot
EXPOSE 8180
ENV PORT=8180
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8180/healthz"]
ENTRYPOINT ["/fastgen"]

# fastproxy needs ffmpeg for Class B (/stable/) demux-stable encode pipes.
FROM debian:bookworm-slim AS fastproxy
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg wget \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --uid 65532 --user-group --no-create-home --shell /usr/sbin/nologin nonroot
COPY --from=build /out/fastproxy /fastproxy
USER nonroot:nonroot
EXPOSE 8181
ENV PORT=8181
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8181/healthz"]
ENTRYPOINT ["/fastproxy"]
