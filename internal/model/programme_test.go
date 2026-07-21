package model

import (
	"testing"
	"time"
)

func TestProgrammeIsValid(t *testing.T) {
	start := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	valid := Programme{Title: "News", Start: start, Stop: start.Add(time.Hour)}
	if !valid.IsValid() {
		t.Fatal("valid programme rejected")
	}

	tests := []Programme{
		{Title: " ", Start: start, Stop: start.Add(time.Hour)},
		{Title: "News", Stop: start.Add(time.Hour)},
		{Title: "News", Start: start},
		{Title: "News", Start: start, Stop: start},
		{Title: "News", Start: start, Stop: start.Add(-time.Hour)},
	}
	for _, programme := range tests {
		if programme.IsValid() {
			t.Fatalf("invalid programme accepted: %+v", programme)
		}
	}
}
