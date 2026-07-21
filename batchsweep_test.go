package main

import (
	"testing"
	"time"
)

func TestSweepPermutationCycleZeroMatchesCLAUDEMdExample(t *testing.T) {
	got := sweepPermutation(0)
	want := [5]int{1, 10, 2, 5, 3}
	if got != want {
		t.Fatalf("sweepPermutation(0) = %v, want %v", got, want)
	}
}

func TestBatchSizeForDayAlwaysAValidSize(t *testing.T) {
	valid := map[int]bool{1: true, 2: true, 3: true, 5: true, 10: true}
	for day := 0; day < 50; day++ {
		size := batchSizeForDay(day)
		if !valid[size] {
			t.Errorf("batchSizeForDay(%d) = %d, not one of the 5 swept sizes", day, size)
		}
	}
}

func TestBatchSizeForDayEqualAllocationOverThirtyDays(t *testing.T) {
	counts := make(map[int]int)
	for day := 0; day < 30; day++ {
		counts[batchSizeForDay(day)]++
	}
	for _, size := range sweepSizes {
		if counts[size] != 6 {
			t.Errorf("size %d occurred %d times over 30 days, want exactly 6", size, counts[size])
		}
	}
}

func TestBatchSizeForDayNegativeClampsToZero(t *testing.T) {
	if got, want := batchSizeForDay(-5), batchSizeForDay(0); got != want {
		t.Errorf("batchSizeForDay(-5) = %d, want %d (clamped to day 0)", got, want)
	}
}

func TestDayIndex(t *testing.T) {
	start := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		want int
	}{
		{"same day, later time", time.Date(2026, 7, 21, 23, 0, 0, 0, time.UTC), 0},
		{"multi-day", time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC), 4},
		{"just before UTC midnight rollover", time.Date(2026, 7, 21, 23, 59, 0, 0, time.UTC), 0},
		{"just after UTC midnight rollover", time.Date(2026, 7, 22, 0, 1, 0, 0, time.UTC), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dayIndex(start, tt.now); got != tt.want {
				t.Errorf("dayIndex(%v, %v) = %d, want %d", start, tt.now, got, tt.want)
			}
		})
	}
}
