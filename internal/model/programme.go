package model

import "time"

// Programme is a guide entry keyed by NormalizedID of its channel.
type Programme struct {
	ChannelID string    `json:"channel_id"` // normalized channel id
	Title     string    `json:"title"`
	Desc      string    `json:"desc,omitempty"`
	Start     time.Time `json:"start"`
	Stop      time.Time `json:"stop"`
}
