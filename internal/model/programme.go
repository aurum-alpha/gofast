package model

import "time"

// Programme is a guide entry keyed by NormalizedID of its channel.
type Programme struct {
	ChannelID string // normalized channel id
	Title     string
	Desc      string
	Start     time.Time
	Stop      time.Time
}
