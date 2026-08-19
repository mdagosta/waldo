// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestIndexProgressReportsSmallSelectionAsOneRange(t *testing.T) {
	for current := 1; current < 4; current++ {
		if _, _, report := indexProgressRange(current, 4); report {
			t.Fatalf("small selection reported incomplete range at %d/4", current)
		}
	}
	if start, end, report := indexProgressRange(4, 4); !report || start != 1 || end != 4 {
		t.Fatalf("small range = %d-%d, report %t", start, end, report)
	}
}

func TestIndexProgressReportsCompleteLargeRanges(t *testing.T) {
	want := map[int][2]int{25: {1, 25}, 50: {26, 50}, 75: {51, 75}, 100: {76, 100}, 107: {101, 107}}
	for current := 1; current <= 107; current++ {
		start, end, report := indexProgressRange(current, 107)
		rangeWant, wanted := want[current]
		if report != wanted || wanted && (start != rangeWant[0] || end != rangeWant[1]) {
			t.Fatalf("range at %d/107 = %d-%d, report %t, want %v", current, start, end, report, rangeWant)
		}
	}
}
