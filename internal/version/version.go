// Package version holds build identity injected at link time via -ldflags -X.
//
// Defaults are for local `go test` / unset builds. CI sets all four via the
// fleet's job-go-build, which derives this package's import path from go.mod:
// every Go repo in the fleet stamps <module>/internal/version, so there is
// nothing per-repo to configure and nothing for two repos to disagree about.
package version

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
type Info struct {
	Version string `json:"version"`
	Build   string `json:"build"`
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
}

// Current returns the process build identity.
func Current() Info {
	return Info{
		Version: Version,
		Build:   Build,
		Commit:  Commit,
		BuiltAt: BuiltAt,
	}
}
