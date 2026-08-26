// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package host

import "fmt"

func physicalMemory() (uint64, error) {
	return 0, fmt.Errorf("physical memory detection is unavailable on this platform")
}
