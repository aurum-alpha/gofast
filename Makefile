# Local-dev convenience wrapper.
#
# CI no longer calls these targets. The standard Go capabilities now live in
# aurum-alpha/workflows as shared single-job workflows (job-go-mod,
# job-go-gofmt, job-go-vet, job-go-test-unit), which run the native toolchain
# themselves — so this repo carries no wrapper script for them and there is
# no per-repo layer to drift from the shared definition. The targets below
# invoke the same native commands for local use.
#
# build is the exception: it is genuinely repo-specific (UI build plus
# version stamping), so scripts/build.sh stays the entrypoint for CI and
# local alike.

.PHONY: go-mod build gofmt vet test-unit

go-mod:
	go mod download
	go mod verify

build:
	./scripts/build.sh

gofmt:
	@dirty="$$(gofmt -l .)"; \
	if [ -n "$$dirty" ]; then \
		printf 'gofmt needed on:\n%s\n' "$$dirty" >&2; \
		exit 1; \
	fi

vet:
	go vet ./...

test-unit:
	mkdir -p coverage
	go test ./... -race -covermode=atomic -coverprofile=coverage/go-unit.out
