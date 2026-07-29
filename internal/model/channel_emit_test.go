package model

import (
	"regexp"
	"testing"
)

func TestChannelEmitValidateAndZero(t *testing.T) {
	n := -1
	if err := (ChannelEmit{Number: &n}).Validate(); err == nil {
		t.Fatal("expected negative number error")
	}
	if err := (ChannelEmit{Export: "maybe"}).Validate(); err == nil {
		t.Fatal("expected invalid export error")
	}
	if !(ChannelEmit{}).IsZero() {
		t.Fatal("empty should be zero")
	}
	if (ChannelEmit{Export: ExportEnabled}).IsZero() {
		t.Fatal("enabled should not be zero")
	}
}

func TestApplyChannelEmitPresentation(t *testing.T) {
	num := 42
	chs := []Channel{
		{NormalizedID: "a", Name: "Upstream", OffsetNumber: 1001, LogoURL: "https://up/a.png"},
		{NormalizedID: "b", Name: "Other", OffsetNumber: 1002},
	}
	emits := map[string]ChannelEmit{
		"a": {Name: "Custom", Number: &num, LogoURL: "https://custom/a.png"},
	}
	out := ApplyChannelEmitPresentation(chs, emits)
	if out[0].Name != "Upstream" {
		t.Fatalf("upstream name mutated: %q", out[0].Name)
	}
	if out[0].EmittedName != "Custom" || out[0].DisplayName() != "Custom" {
		t.Fatalf("emitted name: %+v", out[0])
	}
	if out[0].OffsetNumber != 42 || out[0].LogoURL != "https://custom/a.png" {
		t.Fatalf("presentation: %+v", out[0])
	}
	if out[0].EmitDefaults == nil || out[0].EmitDefaults.Name != "Upstream" || out[0].EmitDefaults.Number != 1001 {
		t.Fatalf("defaults: %+v", out[0].EmitDefaults)
	}
	if out[1].EmittedName != "" || out[1].OffsetNumber != 1002 {
		t.Fatalf("untouched: %+v", out[1])
	}
}

func TestApplyChannelEmitExportPrecedence(t *testing.T) {
	re := regexp.MustCompile("(?i)blocked")
	cases := []struct {
		name      string
		ch        Channel
		emit      ChannelEmit
		wantExcl  bool
		wantReas  FilterReason
		wantForce bool
	}{
		{
			name: "auto keeps exclusion",
			ch: Channel{
				NormalizedID:  "a",
				Excluded:      true,
				FilterReason:  ExclusionMatched(re),
				FilterReasons: []FilterReason{ExclusionMatched(re)},
			},
			wantExcl: true,
			wantReas: ExclusionMatched(re),
		},
		{
			name: "enabled clears exclusion",
			ch: Channel{
				NormalizedID:  "a",
				Excluded:      true,
				FilterReason:  ExclusionMatched(re),
				FilterReasons: []FilterReason{ExclusionMatched(re)},
			},
			emit:      ChannelEmit{Export: ExportEnabled},
			wantExcl:  false,
			wantForce: true,
		},
		{
			name: "disabled excludes",
			ch: Channel{
				NormalizedID: "a",
				Name:         "Ok",
				StreamURL:    "https://ok",
			},
			emit:     ChannelEmit{Export: ExportDisabled},
			wantExcl: true,
			wantReas: FilterReasonEmitDisabled,
		},
		{
			name: "dedupe disabled",
			ch: Channel{
				NormalizedID: "a",
				Name:         "Ok",
				StreamURL:    "https://ok",
			},
			emit:     ChannelEmit{Export: ExportDisabled, Dedupe: true},
			wantExcl: true,
			wantReas: FilterReasonDuplicate,
		},
		{
			name: "enabled does not clear DRM",
			ch: Channel{
				NormalizedID:  "a",
				Excluded:      true,
				FilterReason:  FilterReasonDRM,
				FilterReasons: []FilterReason{FilterReasonDRM},
			},
			emit:      ChannelEmit{Export: ExportEnabled},
			wantExcl:  true,
			wantReas:  FilterReasonDRM,
			wantForce: true,
		},
		{
			name: "enabled clears disabled group",
			ch: Channel{
				NormalizedID:  "a",
				Excluded:      true,
				FilterReason:  DisabledGroupReason("News"),
				FilterReasons: []FilterReason{DisabledGroupReason("News")},
			},
			emit:      ChannelEmit{Export: ExportEnabled},
			wantExcl:  false,
			wantForce: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emits := map[string]ChannelEmit{tc.ch.NormalizedID: tc.emit}
			out := ApplyChannelEmitPreExport([]Channel{tc.ch}, emits)
			if out[0].Excluded != tc.wantExcl {
				t.Fatalf("excluded=%v want %v", out[0].Excluded, tc.wantExcl)
			}
			if tc.wantReas != "" && out[0].FilterReason != tc.wantReas {
				t.Fatalf("reason=%q want %q", out[0].FilterReason, tc.wantReas)
			}
			if out[0].ForceInclude != tc.wantForce {
				t.Fatalf("force=%v want %v", out[0].ForceInclude, tc.wantForce)
			}
		})
	}
}

func TestApplyChannelEmitGroupWins(t *testing.T) {
	chs := []Channel{{
		NormalizedID: "a",
		Group:        "NEWS",
		EmittedGroup: "News",
	}}
	out := ApplyChannelEmitGroup(chs, map[string]ChannelEmit{
		"a": {Group: "My News"},
	})
	if out[0].EmittedGroup != "My News" {
		t.Fatalf("group=%q", out[0].EmittedGroup)
	}
	if out[0].EmitDefaults == nil || out[0].EmitDefaults.Group != "News" {
		t.Fatalf("defaults=%+v", out[0].EmitDefaults)
	}
}

func TestPaintChannelEmit(t *testing.T) {
	chs := []Channel{{NormalizedID: "a", Name: "A"}}
	out := PaintChannelEmit(chs, map[string]ChannelEmit{
		"a": {Name: "Custom"},
	})
	if out[0].Emit == nil || out[0].Emit.Name != "Custom" {
		t.Fatalf("%+v", out[0].Emit)
	}
}
