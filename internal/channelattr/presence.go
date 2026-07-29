package channelattr

import "github.com/j27-aurum/gofast/internal/model"

// Presence is the JSON value for KindPresence: whether a normalized id is in
// the provider catalog (not whether it is exported to M3U).
type Presence struct {
	State string `json:"state"` // present | absent
	Name  string `json:"name,omitempty"`
	TvgID string `json:"tvg_id,omitempty"`
}

// Presence state values (also used on model.Channel.Presence).
const (
	PresencePresent = "present"
	PresenceAbsent  = "absent"
)

// IsPresent reports whether State is present.
func (p Presence) IsPresent() bool {
	return p.State == PresencePresent
}

// GhostChannel builds a UI-only Channel for an absent catalog membership.
func GhostChannel(provider model.ProviderID, channelID string, p Presence) model.Channel {
	name := p.Name
	if name == "" {
		name = channelID
	}
	ch := model.Channel{
		Provider:     provider,
		ID:           channelID,
		NormalizedID: channelID,
		Name:         name,
		Presence:     PresenceAbsent,
	}
	ch.AddFilterReason(model.FilterReasonAbsent)
	return ch
}

// AbsentEntry is one Current presence row with state=absent.
type AbsentEntry struct {
	Provider  model.ProviderID
	ChannelID string
	Presence  Presence
}
