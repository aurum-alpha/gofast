# Canonical CI entrypoints — workflow YAML calls only these targets (and the
# web/package.json scripts); repo-specific flags live here, never inline in CI.

BUILD_NUMBER ?= local
GIT_COMMIT   ?= $(shell git rev-parse --short=7 HEAD 2>/dev/null)
BUILT_AT     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -s -w \
  -X github.com/j27-aurum/gofast/internal/version.Build=$(BUILD_NUMBER) \
  -X github.com/j27-aurum/gofast/internal/version.Commit=$(GIT_COMMIT) \
  -X github.com/j27-aurum/gofast/internal/version.BuiltAt=$(BUILT_AT)

.PHONY: go-mod build gofmt vet test-unit

go-mod:
	go mod download
	go mod verify

# Binaries expect internal/ui/dist to be populated (go:embed); CI builds the UI
# first (npm run build in web/) and hands the dist to downstream jobs.
build:
	go build ./...
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/fastgen ./cmd/fastgen
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/fastproxy ./cmd/fastproxy
	chmod 755 bin/fastgen bin/fastproxy

gofmt:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-clean:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	go vet ./...

test-unit:
	go test ./... -cover
