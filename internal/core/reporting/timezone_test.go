package reporting

import (
	"testing"
	"time"
)

func TestLoadLocationSupportsDefaultReportTimezone(t *testing.T) {
	loc, err := LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load Asia/Kolkata: %v", err)
	}
	_, offset := time.Date(2026, 6, 4, 8, 0, 0, 0, loc).Zone()
	if offset != 5*60*60+30*60 {
		t.Fatalf("expected Asia/Kolkata offset +05:30, got %d seconds", offset)
	}
}
