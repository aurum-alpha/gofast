package refresh

import "time"

// ClampInterval caps configured refresh interval to ≤ half of guideHorizon
// (floor minInterval). When horizon is unknown (≤0), only the minInterval
// floor / fallback for tiny configured values applies.
func ClampInterval(configured, horizon time.Duration) (effective time.Duration, clamped bool) {
	effective = configured
	if effective < minInterval {
		effective = 6 * time.Hour
	}
	if horizon <= 0 {
		return effective, effective != configured && configured < minInterval
	}
	maxInterval := horizon / 2
	if maxInterval < minInterval {
		maxInterval = minInterval
	}
	if effective > maxInterval {
		return maxInterval, true
	}
	return effective, false
}

// resolveHorizon prefers empirical GuideEnd−FetchedAt, else declared expected.
func resolveHorizon(empirical, expected time.Duration) time.Duration {
	if empirical > 0 {
		return empirical
	}
	return expected
}
