package model

// Classification is the stream dialect / playback-path bucket.
// See docs/TERMINOLOGY.md for plain-language definitions.
type Classification string

const (
	ClassNative        Classification = "NATIVE"
	ClassAmagiSSAI     Classification = "AMAGI_SSAI"
	ClassSession       Classification = "SESSION"
	ClassXumoSSAI      Classification = "XUMO_SSAI"
	ClassDRM           Classification = "DRM"
	ClassDistroResolve Classification = "DISTRO_RESOLVE"
	ClassStirrResolve  Classification = "STIRR_RESOLVE"

	// classBeaconLegacy is the pre-J27-49 wire value for Amagi SSAI.
	// Canonical maps it to ClassAmagiSSAI; do not emit this for new writes.
	classBeaconLegacy Classification = "BEACON"
)

// ProxyKind is how FASTProxy (or gen emission) should treat a dialect under
// selective proxying. See docs/TERMINOLOGY.md and internal/proxy package docs.
type ProxyKind int

const (
	// ProxyNone means emit upstream; under proxy_all the proxy 302s to upstream.
	ProxyNone ProxyKind = iota
	// ProxyAmagiRewrite is beacon playlist rewrite + /seg shuttle (AMAGI_SSAI).
	ProxyAmagiRewrite
	// ProxySessionMint is Google DAI mint-on-tune-in then 302 to stream_manifest.
	ProxySessionMint
	// ProxyDistroResolve is DistroTV jsrdn feed resolve at tune-in, then 302 or rewrite.
	ProxyDistroResolve
	// ProxyStirrResolve is STIRR POST /playable resolve at tune-in, then 302 or rewrite.
	ProxyStirrResolve
)

// Canonical returns the current wire value for c (maps legacy BEACON → AMAGI_SSAI).
func (c Classification) Canonical() Classification {
	if c == classBeaconLegacy {
		return ClassAmagiSSAI
	}
	return c
}

// Known reports whether c is a recognized dialect (empty counts as known/unset).
func (c Classification) Known() bool {
	switch c.Canonical() {
	case "", ClassNative, ClassAmagiSSAI, ClassSession, ClassXumoSSAI, ClassDRM, ClassDistroResolve, ClassStirrResolve:
		return true
	default:
		return false
	}
}

// ProxyKind reports the FASTProxy branch for this dialect.
func (c Classification) ProxyKind() ProxyKind {
	switch c.Canonical() {
	case ClassAmagiSSAI:
		return ProxyAmagiRewrite
	case ClassSession:
		return ProxySessionMint
	case ClassDistroResolve:
		return ProxyDistroResolve
	case ClassStirrResolve:
		return ProxyStirrResolve
	default:
		return ProxyNone
	}
}

// RequiresAmagiProxy reports whether this dialect uses the Amagi SSAI rewrite path.
func (c Classification) RequiresAmagiProxy() bool {
	return c.ProxyKind() == ProxyAmagiRewrite
}

// RequiresProxy reports whether selective emission must embed a proxy /stream URL.
// True for Amagi rewrite, SESSION mint, Distro resolve, and STIRR resolve;
// false for NATIVE / XUMO_SSAI / DRM.
func (c Classification) RequiresProxy() bool {
	switch c.ProxyKind() {
	case ProxyAmagiRewrite, ProxySessionMint, ProxyDistroResolve, ProxyStirrResolve:
		return true
	default:
		return false
	}
}

// ScheduleSegmentHealth reports whether scheduled Health L1 segment probes may
// apply for this dialect. Dialects that need FASTProxy are scheduled only when
// EmittedURL is set (see health.l1ShouldSchedule) so probes hit /stream/… rather
// than opaque/upstream URLs. SESSION stays off the timer (DAI mint = paid fake
// tune). DRM is never probed.
func (c Classification) ScheduleSegmentHealth() bool {
	switch c.Canonical() {
	case ClassNative, ClassXumoSSAI, ClassAmagiSSAI, ClassDistroResolve, ClassStirrResolve:
		return true
	default:
		return false
	}
}
