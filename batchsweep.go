package main

import "time"

// sweepSizes is the fixed condition set batch size is swept across
// (CLAUDE.md §4.4) - co-equal conditions, none over-weighted.
var sweepSizes = [5]int{1, 2, 3, 5, 10}

// sweepPermutation returns the batch-size order for one 5-day cycle: the
// sizes rotated left by cycle positions, then zigzagged from the outside in
// (max-spread - CLAUDE.md §4.5's own example is cycle 0's output,
// [1,10,2,5,3]). Rotating per cycle means the permutation itself changes
// cycle to cycle, not just the day-of-week alignment.
func sweepPermutation(cycle int) [5]int {
	shift := cycle % len(sweepSizes)
	var rotated [5]int
	for i := range rotated {
		rotated[i] = sweepSizes[(i+shift)%len(sweepSizes)]
	}

	var out [5]int
	lo, hi := 0, len(rotated)-1
	for i := range out {
		if i%2 == 0 {
			out[i] = rotated[lo]
			lo++
		} else {
			out[i] = rotated[hi]
			hi--
		}
	}
	return out
}

// batchSizeForDay maps a day index (days since the sweep's configured start)
// to that day's batch size. Pure function of an integer - no wall-clock
// inside it, so it's unit-testable without faking time.
func batchSizeForDay(dayIndex int) int {
	if dayIndex < 0 {
		dayIndex = 0
	}
	cycle := dayIndex / len(sweepSizes)
	pos := dayIndex % len(sweepSizes)
	return sweepPermutation(cycle)[pos]
}

// dayIndex counts whole calendar days between start and now, both taken as
// UTC dates (not a raw duration divide) so a run landing a few minutes
// either side of midnight doesn't skip or repeat a day.
func dayIndex(start, now time.Time) int {
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return int(nowDate.Sub(startDate).Hours() / 24)
}
