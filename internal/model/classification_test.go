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

func TestScheduleSegmentHealth(t *testing.T) {
	for _, c := range []Classification{ClassNative, ClassSession, ClassXumoSSAI} {
		if !c.ScheduleSegmentHealth() {
			t.Fatalf("%q should schedule L2", c)
		}
	}
	for _, c := range []Classification{ClassAmagiSSAI, classBeaconLegacy, ClassDRM, ""} {
		if c.ScheduleSegmentHealth() {
			t.Fatalf("%q should not schedule L2", c)
		}
	}
}
