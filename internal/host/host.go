// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package host reports local machine resources that are independent of any
// training framework. Accelerator and runtime facts remain owned by training.
package host

import (
	"fmt"
	"runtime"
)

type Facts struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	CPUs         int    `json:"cpus"`
	MemoryBytes  uint64 `json:"memory_bytes"`
	DiskBytes    uint64 `json:"disk_bytes"`
	DiskFree     uint64 `json:"disk_free_bytes"`
}

func Inspect() (Facts, error) {
	memory, err := physicalMemory()
	if err != nil {
		return Facts{}, fmt.Errorf("inspect host memory: %w", err)
	}
	disk, free, err := filesystemCapacity()
	if err != nil {
		return Facts{}, fmt.Errorf("inspect host filesystem: %w", err)
	}
	return Facts{
		OS: runtime.GOOS, Architecture: runtime.GOARCH, CPUs: runtime.NumCPU(),
		MemoryBytes: memory, DiskBytes: disk, DiskFree: free,
	}, nil
}
