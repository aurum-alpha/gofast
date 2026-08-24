// Package version holds build identity injected at link time via -ldflags -X.
//
// Defaults are for local `go test` / unset builds. CI sets all four via the
// fleet's job-go-build, which derives this package's import path from go.mod:
// every Go repo in the fleet stamps <module>/internal/version, so there is
// nothing per-repo to configure and nothing for two repos to disagree about.
package version

import "runtime"

// Set via:
//
//	-X github.com/j27-aurum/gofast/internal/version.Version=…
//	-X github.com/j27-aurum/gofast/internal/version.Build=…
//	-X github.com/j27-aurum/gofast/internal/version.Commit=…
//	-X github.com/j27-aurum/gofast/internal/version.BuiltAt=…
//
// Version is "dev" here and in CI: this repo has no .version file because
// nothing outside it decides anything from the string (CI_STANDARD Principle
// 16). Build, Commit and BuiltAt answer "what is this" exactly, which is what
// provenance is for.
var (
	Version = "dev"
	Build   = "local"
	Commit  = ""
	BuiltAt = ""
)

// Info is the JSON shape for /healthz and /api/status.
//
// Platform and GoVersion are additive: a client reading the old shape keeps
// working, and on a multi-architecture deployment "which build am I talking
// to" is exactly the question a health endpoint should be able to answer.
type Info struct {
	Version   string `json:"version"`
	Build     string `json:"build"`
	Commit    string `json:"commit,omitempty"`
	BuiltAt   string `json:"built_at,omitempty"`
	Platform  string `json:"platform"`
	GoVersion string `json:"go_version"`
}

// Platform is the OS/arch this binary was compiled for, e.g. "linux/arm64".
//
// DERIVED, never stamped, and the distinction matters now that one image
// carries two architectures. runtime.GOOS and runtime.GOARCH are compile-time
// constants the linker has already baked in, so this cannot disagree with the
// binary reporting it. A stamped equivalent could: -X writes whatever the
// build passed, and -X on a symbol that does not exist is ignored in silence,
// so a wrong or missing goarch input would yield a binary that confidently
// misreports itself. Stamp what the toolchain cannot know; read what it does.
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// GoVersion is the toolchain that built this binary, e.g. "go1.26.6".
func GoVersion() string { return runtime.Version() }

// Current returns the process build identity.
func Current() Info {
	return Info{
		Version:   Version,
		Build:     Build,
		Commit:    Commit,
		BuiltAt:   BuiltAt,
		Platform:  Platform(),
		GoVersion: GoVersion(),
	}
}
