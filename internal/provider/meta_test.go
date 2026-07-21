package provider

import (
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestMetaOfRetainsHistoricalAssignments(t *testing.T) {
	lineup := Lineup{
		Channels: []model.Channel{{
			NormalizedID:   "current",
			Classification: model.ClassNative,
		}},
		FetchedAt: time.Now(),
		SyntheticChannelNumbers: ChannelNumberAssignments{
			"current": 5000,
			"gone":    5001,
		},
	}
	meta := MetaOf(lineup)
	if meta.SyntheticChannelNumbers["gone"] != 5001 {
		t.Fatalf("historical assignment missing: %+v", meta.SyntheticChannelNumbers)
	}
	meta.SyntheticChannelNumbers["current"] = 9999
	if lineup.SyntheticChannelNumbers["current"] != 5000 {
		t.Fatal("MetaOf returned an aliased assignment map")
	}
}
