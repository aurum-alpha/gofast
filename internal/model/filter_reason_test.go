package model

import (
	"regexp"
	"testing"
)

func TestAddFilterReasonAccumulatesAndPrimary(t *testing.T) {
	var ch Channel
	ch.AddFilterReason(ExclusionMatched(regexp.MustCompile("(?i)foo")))
	ch.AddFilterReason(FilterReasonDuplicate)
	ch.AddFilterReason(FilterReasonDRM)
	if len(ch.FilterReasons) != 3 {
		t.Fatalf("reasons=%+v", ch.FilterReasons)
	}
	if ch.FilterReason != FilterReasonDRM {
		t.Fatalf("primary=%q", ch.FilterReason)
	}
	if !ch.Excluded {
		t.Fatal("expected excluded")
	}
}

func TestClearSoftKeepsHard(t *testing.T) {
	ch := Channel{}
	ch.AddFilterReason(FilterReasonDuplicate)
	ch.AddFilterReason(FilterReasonDRM)
	ch.ClearSoftFilterReasons()
	if len(ch.FilterReasons) != 1 || ch.FilterReasons[0] != FilterReasonDRM {
		t.Fatalf("%+v", ch.FilterReasons)
	}
}

func TestFilterReasonKinds(t *testing.T) {
	if FilterReasonDuplicate.Kind() != FilterKindDuplicate {
		t.Fatal(FilterReasonDuplicate.Kind())
	}
	if DisabledGroupReason("News").Kind() != FilterKindDisabledGroup {
		t.Fatal(DisabledGroupReason("News").Kind())
	}
	if !FilterReasonNeedsFASTProxy.IsHard() || FilterReasonDuplicate.IsHard() {
		t.Fatal("soft/hard mismatch")
	}
}
