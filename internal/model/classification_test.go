package model

import "testing"

func TestClassificationConstants(t *testing.T) {
	if ClassNative != "NATIVE" || ClassBeacon != "BEACON" || ClassDRM != "DRM" {
		t.Fatalf("unexpected constants")
	}
}
