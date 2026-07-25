package model

import "testing"

func TestClassificationConstants(t *testing.T) {
	if ClassNative != "NATIVE" || ClassAmagiSSAI != "AMAGI_SSAI" || ClassSession != "SESSION" ||
		ClassXumoSSAI != "XUMO_SSAI" || ClassDRM != "DRM" {
		t.Fatalf("unexpected constants")
	}
}

func TestClassificationCanonical(t *testing.T) {
	cases := []struct {
		in, want Classification
	}{
		{ClassNative, ClassNative},
		{ClassAmagiSSAI, ClassAmagiSSAI},
		{classBeaconLegacy, ClassAmagiSSAI},
		{ClassSession, ClassSession},
		{ClassXumoSSAI, ClassXumoSSAI},
		{ClassDRM, ClassDRM},
		{"", ""},
	}
	for _, tc := range cases {
		if got := tc.in.Canonical(); got != tc.want {
			t.Fatalf("Canonical(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestProxyKind(t *testing.T) {
	if ClassAmagiSSAI.ProxyKind() != ProxyAmagiRewrite || classBeaconLegacy.ProxyKind() != ProxyAmagiRewrite {
		t.Fatal("Amagi should be ProxyAmagiRewrite")
	}
	if ClassSession.ProxyKind() != ProxySessionMint {
		t.Fatal("SESSION should be ProxySessionMint")
	}
	for _, c := range []Classification{ClassNative, ClassXumoSSAI, ClassDRM, ""} {
		if c.ProxyKind() != ProxyNone {
			t.Fatalf("%q ProxyKind=%v want ProxyNone", c, c.ProxyKind())
		}
	}
}

func TestRequiresAmagiProxy(t *testing.T) {
	if !ClassAmagiSSAI.RequiresAmagiProxy() || !classBeaconLegacy.RequiresAmagiProxy() {
		t.Fatal("Amagi / legacy BEACON should require Amagi proxy")
	}
	for _, c := range []Classification{ClassNative, ClassSession, ClassXumoSSAI, ClassDRM, ""} {
		if c.RequiresAmagiProxy() {
			t.Fatalf("%q should not require Amagi proxy", c)
		}
	}
}

func TestRequiresProxy(t *testing.T) {
	for _, c := range []Classification{ClassAmagiSSAI, classBeaconLegacy, ClassSession} {
		if !c.RequiresProxy() {
			t.Fatalf("%q should RequireProxy", c)
		}
	}
	for _, c := range []Classification{ClassNative, ClassXumoSSAI, ClassDRM, ""} {
		if c.RequiresProxy() {
			t.Fatalf("%q should not RequireProxy", c)
		}
	}
}

func TestScheduleSegmentHealth(t *testing.T) {
	for _, c := range []Classification{ClassNative, ClassXumoSSAI, ClassAmagiSSAI, classBeaconLegacy} {
		if !c.ScheduleSegmentHealth() {
			t.Fatalf("%q should schedule L1 (Amagi only when EmittedURL set at sweep)", c)
		}
	}
	for _, c := range []Classification{ClassSession, ClassDRM, ""} {
		if c.ScheduleSegmentHealth() {
			t.Fatalf("%q should not schedule L1", c)
		}
	}
}
