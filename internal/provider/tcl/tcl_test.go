package tcl

import (
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestDefaultSettings(t *testing.T) {
	t.Parallel()
	s := DefaultSettings()
	if s.ID != model.ProviderTCL || s.Label != "TCL" {
		t.Fatalf("id/label: %+v", s)
	}
	if s.SynthesizeChannelNumbers != 10000 || s.MinChannels != 200 {
		t.Fatalf("numbering/min: synthesize=%d min=%d", s.SynthesizeChannelNumbers, s.MinChannels)
	}
	if s.RefreshInterval != 6*time.Hour || s.ExpectedGuideHorizon != 48*time.Hour {
		t.Fatalf("schedule: refresh=%v horizon=%v", s.RefreshInterval, s.ExpectedGuideHorizon)
	}
	if source.M3UURL == "" || source.EPGURL == "" || source.EPGGzip {
		t.Fatalf("source URLs/gzip: %+v", source)
	}
}
