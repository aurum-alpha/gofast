package model

// Classification is the stream dialect / playback-path bucket.
type Classification string

const (
	ClassNative    Classification = "NATIVE"
	ClassAmagiSSAI Classification = "AMAGI_SSAI"
	ClassSession   Classification = "SESSION"
	ClassXumoSSAI  Classification = "XUMO_SSAI"
	ClassDRM       Classification = "DRM"

	// classBeaconLegacy is the pre-J27-49 wire value for Amagi SSAI.
	// Canonical maps it to ClassAmagiSSAI; do not emit this for new writes.
	classBeaconLegacy Classification = "BEACON"
)

// Canonical returns the current wire value for c (maps legacy BEACON → AMAGI_SSAI).
func (c Classification) Canonical() Classification {
	if c == classBeaconLegacy {
		return ClassAmagiSSAI
	}
	return c
}

// RequiresAmagiProxy reports whether this dialect uses the Amagi SSAI rewrite path.
func (c Classification) RequiresAmagiProxy() bool {
	return c.Canonical() == ClassAmagiSSAI
}

// ScheduleSegmentHealth reports whether scheduled L2 segment probes apply.
// Amagi SSAI is excluded (avoid firing impression beacons); DRM is never probed.
func (c Classification) ScheduleSegmentHealth() bool {
	switch c.Canonical() {
	case ClassNative, ClassSession, ClassXumoSSAI:
		return true
	default:
		return false
	}
}
