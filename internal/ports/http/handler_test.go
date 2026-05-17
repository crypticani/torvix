package httpapi

import (
	"testing"
	"time"
)

func TestDefaultReportRange(t *testing.T) {
	now := time.Date(2026, 5, 17, 15, 30, 0, 0, time.UTC) // Sunday
	tests := []struct {
		period   string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{
			period:   "daily",
			wantFrom: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		},
		{
			period:   "weekly",
			wantFrom: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			period:   "monthly",
			wantFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			gotFrom, gotTo := defaultReportRange(tt.period, now)
			if !gotFrom.Equal(tt.wantFrom) || !gotTo.Equal(tt.wantTo) {
				t.Fatalf("range mismatch: got %s - %s, want %s - %s", gotFrom, gotTo, tt.wantFrom, tt.wantTo)
			}
		})
	}
}
