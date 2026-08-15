# Local-dev convenience wrapper. The canonical CI entrypoints are the
# scripts under scripts/ — workflow YAML calls those directly (the runner
# image does not ship make); repo-specific flags live in the scripts,
# never inline in CI.

.PHONY: go-mod build gofmt vet test-unit

go-mod:
	./scripts/go-mod.sh

build:
	./scripts/build.sh

gofmt:
	./scripts/gofmt.sh

vet:
	./scripts/vet.sh

test-unit:
	./scripts/test-unit.sh
