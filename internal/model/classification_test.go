package model

import "testing"

func TestClassificationConstants(t *testing.T) {
	if ClassNative != "NATIVE" || ClassAmagiSSAI != "AMAGI_SSAI" || ClassSession != "SESSION" ||
		ClassXumoSSAI != "XUMO_SSAI" || ClassDRM != "DRM" || ClassDistroResolve != "DISTRO_RESOLVE" ||
		ClassStirrResolve != "STIRR_RESOLVE" {
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
		{ClassDistroResolve, ClassDistroResolve},
		{ClassStirrResolve, ClassStirrResolve},
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
	if ClassDistroResolve.ProxyKind() != ProxyDistroResolve {
		t.Fatal("DISTRO_RESOLVE should be ProxyDistroResolve")
	}
	if ClassStirrResolve.ProxyKind() != ProxyStirrResolve {
		t.Fatal("STIRR_RESOLVE should be ProxyStirrResolve")
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
	for _, c := range []Classification{ClassNative, ClassSession, ClassXumoSSAI, ClassDRM, ClassDistroResolve, ClassStirrResolve, ""} {
		if c.RequiresAmagiProxy() {
			t.Fatalf("%q should not require Amagi proxy", c)
		}
	}
}

func TestClassificationKnown(t *testing.T) {
	for _, c := range []Classification{"", ClassNative, ClassAmagiSSAI, ClassSession, ClassXumoSSAI, ClassDRM, ClassDistroResolve, ClassStirrResolve, classBeaconLegacy} {
		if !c.Known() {
			t.Fatalf("%q should be known", c)
		}
	}
	if (Classification("WEIRD")).Known() {
		t.Fatal("unknown class should not be Known")
	}
}

func TestRequiresProxy(t *testing.T) {
	for _, c := range []Classification{ClassAmagiSSAI, classBeaconLegacy, ClassSession, ClassDistroResolve, ClassStirrResolve} {
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
	for _, c := range []Classification{
		ClassNative, ClassXumoSSAI, ClassAmagiSSAI, classBeaconLegacy,
		ClassDistroResolve, ClassStirrResolve,
	} {
		if !c.ScheduleSegmentHealth() {
			t.Fatalf("%q should schedule L1 when EmittedURL set for proxy dialects", c)
		}
	}
	for _, c := range []Classification{ClassSession, ClassDRM, ""} {
		if c.ScheduleSegmentHealth() {
			t.Fatalf("%q should not schedule L1", c)
		}
	}
}
