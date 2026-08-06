package stirr

import "testing"

func TestOpaqueRoundTrip(t *testing.T) {
	u := OpaqueStreamURL("5407")
	if u != "stirr://channel/5407" {
		t.Fatalf("OpaqueStreamURL=%q", u)
	}
	id, ok := ParseOpaque(u)
	if !ok || id != "5407" {
		t.Fatalf("ParseOpaque=%q ok=%v", id, ok)
	}
	if _, ok := ParseOpaque("https://example.com"); ok {
		t.Fatal("expected reject http URL")
	}
	if _, ok := ParseOpaque("stirr://channel/"); ok {
		t.Fatal("expected reject empty id")
	}
}

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()
	if s.ID != "stirr" || s.Label != "STIRR" {
		t.Fatalf("id/label: %+v", s)
	}
	if s.IsEnabled() {
		t.Fatal("expected disabled by default")
	}
	if s.SynthesizeChannelNumbers != 11000 || s.MinChannels != 50 {
		t.Fatalf("synth/min: %+v", s)
	}
}
