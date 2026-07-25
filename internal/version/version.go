// Package version holds build identity injected at link time via -ldflags -X.
//
// Defaults are for local `go test` / unset builds. CI sets Build to
// github.run_number, Commit to the short SHA, and BuiltAt to a UTC timestamp.
package version

// Set via:
//
//	-X github.com/j27-aurum/gofast/internal/version.Build=…
//	-X github.com/j27-aurum/gofast/internal/version.Commit=…
//	-X github.com/j27-aurum/gofast/internal/version.BuiltAt=…
var (
	Build   = "local"
	Commit  = ""
	BuiltAt = ""
)

// Info is the JSON shape for /healthz and /api/status.
type Info struct {
	Build   string `json:"build"`
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
}

// Current returns the process build identity.
func Current() Info {
	return Info{
		Build:   Build,
		Commit:  Commit,
		BuiltAt: BuiltAt,
	}
}
