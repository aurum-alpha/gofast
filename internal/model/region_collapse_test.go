package model

import "testing"

func TestCollapseRegionalTwinsPrefersEarlierRegion(t *testing.T) {
	chs := []Channel{
		{ID: "CA_news", UpstreamID: "news", Region: "CA", Name: "News CA"},
		{ID: "US_news", UpstreamID: "news", Region: "US", Name: "News US"},
		{ID: "US_other", UpstreamID: "other", Region: "US", Name: "Other"},
		{ID: "solo", Name: "Solo"}, // no upstream/region
	}
	got := CollapseRegionalTwins(chs, []string{"US", "CA"})
	if len(got) != 3 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	ids := map[string]bool{}
	for _, ch := range got {
		ids[ch.ID] = true
	}
	if !ids["US_news"] || ids["CA_news"] || !ids["US_other"] || !ids["solo"] {
		t.Fatalf("ids=%v", ids)
	}
}

func TestCollapseRegionalTwinsNoOpWithoutUpstream(t *testing.T) {
	chs := []Channel{
		{ID: "a", Region: "US", Name: "A"},
		{ID: "b", Region: "CA", Name: "B"},
	}
	got := CollapseRegionalTwins(chs, []string{"US", "CA"})
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
}
