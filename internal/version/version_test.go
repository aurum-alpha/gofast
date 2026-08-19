package version

import "testing"

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
