package version

import "testing"

func TestCurrentDefaults(t *testing.T) {
	got := Current()
	if got.Build != "local" {
		t.Fatalf("Build=%q want local", got.Build)
	}
}

func TestCurrentReflectsVars(t *testing.T) {
	prevB, prevC, prevT := Build, Commit, BuiltAt
	t.Cleanup(func() {
		Build, Commit, BuiltAt = prevB, prevC, prevT
	})
	Build, Commit, BuiltAt = "142", "c2ca3e9", "2026-07-25T18:00:00Z"
	got := Current()
	if got.Build != "142" || got.Commit != "c2ca3e9" || got.BuiltAt != "2026-07-25T18:00:00Z" {
		t.Fatalf("%+v", got)
	}
}
