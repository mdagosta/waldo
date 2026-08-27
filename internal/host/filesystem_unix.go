// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package host

import "golang.org/x/sys/unix"

func filesystemCapacity() (uint64, uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs("/", &stat); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(stat.Bsize)
	return stat.Blocks * blockSize, stat.Bavail * blockSize, nil
}
