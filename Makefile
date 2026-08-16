# Local-dev convenience wrapper.
#
# CI no longer calls these targets. The standard Go capabilities now live in
# aurum-alpha/workflows as shared single-job workflows (job-go-mod,
# job-go-gofmt, job-go-vet, job-go-test-unit), which run the native toolchain
# themselves — so this repo carries no wrapper script for them and there is
# no per-repo layer to drift from the shared definition. The targets below
# invoke the same native commands for local use.
#
# The React app is built by the shared job-node-build in CI, which uploads
# web/dist as an artifact that the Go build downloads into internal/ui/dist.
# `make ui` is the local equivalent of that hand-off; `make build` runs it
# first so a local build embeds a current UI.
#
# scripts/build.sh stays repo-specific: it stamps internal/version via
# -ldflags, which no shared job can know about.

.PHONY: go-mod ui build gofmt vet test-unit

go-mod:
	go mod download
	go mod verify

ui:
	cd web && pnpm install --frozen-lockfile && pnpm run build
	rm -rf internal/ui/dist
	mkdir -p internal/ui
	cp -R web/dist internal/ui/dist

build: ui
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
