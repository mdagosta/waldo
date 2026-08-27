// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package host

import "testing"

func TestInspectReportsCurrentHostCapacity(t *testing.T) {
	facts, err := Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if facts.OS == "" || facts.Architecture == "" || facts.CPUs < 1 || facts.MemoryBytes == 0 || facts.DiskBytes == 0 || facts.DiskFree > facts.DiskBytes {
		t.Fatalf("facts = %+v", facts)
	}
}
