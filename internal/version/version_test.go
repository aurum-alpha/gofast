package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestCurrentDefaults(t *testing.T) {
	got := Current()
	if got.Build != "local" {
		t.Fatalf("Build=%q want local", got.Build)
	}
	if got.Version != "dev" {
		t.Fatalf("Version=%q want dev", got.Version)
	}
}

func TestCurrentReflectsVars(t *testing.T) {
	prevV, prevB, prevC, prevT := Version, Build, Commit, BuiltAt
	t.Cleanup(func() {
		Version, Build, Commit, BuiltAt = prevV, prevB, prevC, prevT
	})
	// All four, because -X on a var this struct does not copy through is
	// silently ignored by the linker and would surface as an empty field in
	// /healthz rather than as a build failure.
	Version, Build, Commit, BuiltAt = "1.2.3", "142", "c2ca3e9", "2026-07-25T18:00:00Z"
	got := Current()
	if got.Version != "1.2.3" || got.Build != "142" ||
		got.Commit != "c2ca3e9" || got.BuiltAt != "2026-07-25T18:00:00Z" {
		t.Fatalf("%+v", got)
	}
}

// Platform must track the binary rather than a build input — that is the whole
// reason it is derived. Asserting against runtime's own constants means a
// change to Platform() that stops reflecting the compiled target fails here
// rather than in a release binary reporting the wrong architecture.
func TestPlatformIsDerived(t *testing.T) {
	want := runtime.GOOS + "/" + runtime.GOARCH
	if got := Platform(); got != want {
		t.Fatalf("Platform() = %q, want %q", got, want)
	}
}

// Current must carry both new fields through. A struct that does not copy a
// value through is the same silent-omission class as an ignored -X: the field
// serialises as empty and /healthz answers the question wrongly rather than
// failing.
func TestCurrentCarriesPlatform(t *testing.T) {
	got := Current()
	if got.Platform != Platform() {
		t.Errorf("Current().Platform = %q, want %q", got.Platform, Platform())
	}
	if got.GoVersion != runtime.Version() {
		t.Errorf("Current().GoVersion = %q, want %q", got.GoVersion, runtime.Version())
	}
	if !strings.HasPrefix(got.GoVersion, "go") {
		t.Errorf("GoVersion = %q, want a go-prefixed toolchain version", got.GoVersion)
	}
}
