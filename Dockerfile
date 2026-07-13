# syntax=docker/dockerfile:1

# BIN_SOURCE=build (default, local compose) | prebuilt (CI packages bin/)
ARG BIN_SOURCE=build

# Default path: compile inside Docker (local `docker compose build`).
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/fastgen ./cmd/fastgen
RUN go build -trimpath -ldflags="-s -w" -o /out/fastproxy ./cmd/fastproxy

# CI path: package prebuilt static binaries from the compile job (bin/).
# Artifact upload/download often strips +x; force mode before the final image.
FROM debian:bookworm-slim AS prebuilt
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY bin/fastgen /out/fastgen
COPY bin/fastproxy /out/fastproxy
RUN chmod 755 /out/fastgen /out/fastproxy

FROM ${BIN_SOURCE} AS artifacts

FROM gcr.io/distroless/static-debian12:nonroot AS fastgen
COPY --from=artifacts /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=artifacts /out/fastgen /fastgen
COPY --from=busybox:1.36.1-musl /bin/wget /wget
EXPOSE 8180
ENV FASTGEN_LISTEN=:8180
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8180/healthz"]
ENTRYPOINT ["/fastgen"]

FROM gcr.io/distroless/static-debian12:nonroot AS fastproxy
COPY --from=artifacts /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=artifacts /out/fastproxy /fastproxy
COPY --from=busybox:1.36.1-musl /bin/wget /wget
EXPOSE 8181
ENV FASTPROXY_LISTEN=:8181
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8181/healthz"]
ENTRYPOINT ["/fastproxy"]
