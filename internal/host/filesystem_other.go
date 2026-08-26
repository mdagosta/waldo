// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package host

import "fmt"

func filesystemCapacity() (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("filesystem capacity detection is unavailable on this platform")
}
