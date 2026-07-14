package model

// Classification is the stream classifier bucket.
type Classification string

const (
	ClassNative Classification = "NATIVE"
	ClassBeacon Classification = "BEACON"
	ClassDRM    Classification = "DRM"
)
