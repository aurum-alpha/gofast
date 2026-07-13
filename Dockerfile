# syntax=docker/dockerfile:1
#
# LOCAL / DEV — build UI + Go from source (docker compose build).
# Production/GHCR uses Dockerfile.prod (CI binaries only). Keep image pins in sync:
#   node:22-bookworm
#   golang:1.23-bookworm
#   gcr.io/distroless/static-debian12:nonroot
#   busybox:1.36.1-musl

FROM node:22-bookworm AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN mkdir -p /src/internal/ui && npm run build

FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/ui/dist/ internal/ui/dist/
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/fastgen ./cmd/fastgen
RUN go build -trimpath -ldflags="-s -w" -o /out/fastproxy ./cmd/fastproxy

FROM gcr.io/distroless/static-debian12:nonroot AS fastgen
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/fastgen /fastgen
COPY --from=busybox:1.36.1-musl /bin/wget /wget
EXPOSE 8180
ENV FASTGEN_LISTEN=:8180
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8180/healthz"]
ENTRYPOINT ["/fastgen"]

FROM gcr.io/distroless/static-debian12:nonroot AS fastproxy
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/fastproxy /fastproxy
COPY --from=busybox:1.36.1-musl /bin/wget /wget
EXPOSE 8181
ENV FASTPROXY_LISTEN=:8181
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8181/healthz"]
ENTRYPOINT ["/fastproxy"]
