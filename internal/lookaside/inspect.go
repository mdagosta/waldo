// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package lookaside

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Stats struct {
	Objects int64 `json:"objects"`
	Bytes   int64 `json:"bytes"`
	Other   int64 `json:"other_files"`
}

type ScrubResult struct {
	Stats
	Verified int64   `json:"verified"`
	Corrupt  []Issue `json:"corrupt,omitempty"`
}

type Issue struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

func (cache *Cache) Stats() (Stats, error) {
	var result Stats
	err := cache.walk(func(path, digest string, info fs.FileInfo) error {
		if digest == "" {
			result.Other++
			return nil
		}
		result.Objects++
		result.Bytes += info.Size()
		return nil
	})
	return result, err
}

// Scrub reads every content-addressed cache object and verifies that its bytes
// match its filename. It reports corruption without deleting or repairing it.
func (cache *Cache) Scrub() (ScrubResult, error) {
	var result ScrubResult
	err := cache.walk(func(path, digest string, info fs.FileInfo) error {
		if digest == "" {
			result.Other++
			return nil
		}
		result.Objects++
		result.Bytes += info.Size()
		if err := VerifyFile(path, digest, info.Size()); err != nil {
			result.Corrupt = append(result.Corrupt, Issue{Path: path, Error: err.Error()})
			return nil
		}
		result.Verified++
		return nil
	})
	return result, err
}

func (cache *Cache) walk(visit func(path, digest string, info fs.FileInfo) error) error {
	if _, err := os.Stat(cache.root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.Walk(cache.root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		digest := filepath.Base(path)
		if validateDigest(digest) != nil {
			digest = ""
		}
		if err := visit(path, digest, info); err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		return nil
	})
}
