// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestIndexProgressShowsEverySmallSelection(t *testing.T) {
	for current := 1; current <= 4; current++ {
		if !shouldReportIndexProgress(current, 4) {
			t.Fatalf("small selection suppressed %d/4", current)
		}
	}
}

func TestIndexProgressLimitsLargeSelections(t *testing.T) {
	want := map[int]bool{1: true, 25: true, 50: true, 75: true, 100: true}
	for current := 1; current <= 100; current++ {
		if got := shouldReportIndexProgress(current, 100); got != want[current] {
			t.Fatalf("report %d/100 = %t, want %t", current, got, want[current])
		}
	}
}
