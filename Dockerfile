# syntax=docker/dockerfile:1

FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/fastgen ./cmd/fastgen
RUN go build -trimpath -ldflags="-s -w" -o /out/fastproxy ./cmd/fastproxy

FROM gcr.io/distroless/static-debian12:nonroot AS fastgen
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/fastgen /fastgen
EXPOSE 8080
ENV FASTGEN_LISTEN=:8080
ENTRYPOINT ["/fastgen"]

FROM gcr.io/distroless/static-debian12:nonroot AS fastproxy
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/fastproxy /fastproxy
EXPOSE 8081
ENV FASTPROXY_LISTEN=:8081
ENTRYPOINT ["/fastproxy"]
