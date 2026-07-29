// Package channelattr stores current + history channel labels (health,
// classification, presence, …) in SQLite outside cache generations.
package channelattr

import (
	"encoding/json"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// Kind identifies which channel label an event updates.
type Kind string

const (
	KindHealth         Kind = "health"
	KindClassification Kind = "classification"
	KindPresence       Kind = "presence"
)

// Event is what producers Emit. Value is kind-specific JSON.
type Event struct {
	Provider  model.ProviderID
	ChannelID string // NormalizedID
	Kind      Kind
	Value     json.RawMessage
	At        time.Time
	Source    string // "probe", "playback", "classifier", "refresh", …
}
