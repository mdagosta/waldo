// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package corpus

import (
	"context"
	"fmt"

	"github.com/openwaldo/waldo/internal/lookaside"
)

type Materialized struct {
	BOM     BOM                  `json:"bom"`
	Objects []MaterializedObject `json:"objects"`
}

type MaterializedObject struct {
	Shard ShardPin `json:"shard"`
	Path  string   `json:"path"`
}

type MaterializeProgress struct {
	Current int
	Total   int
	Shard   ShardPin
	Path    string
}

// Materialize turns an OpenWALDO BOM into verified local object paths. It
// does not revisit the index. Duplicate object hashes share one fetched path
// while retaining each manifest reference in the returned sequence.
func Materialize(ctx context.Context, bom BOM, cache *lookaside.Cache, progress func(MaterializeProgress)) (Materialized, error) {
	if cache == nil {
		return Materialized{}, fmt.Errorf("lookaside scratch is required")
	}
	result := Materialized{BOM: bom, Objects: make([]MaterializedObject, 0, len(bom.Shards))}
	fetched := map[string]string{}
	sizes := map[string]int64{}
	for position, shard := range bom.Shards {
		path := fetched[shard.SHA256]
		if path != "" {
			if sizes[shard.SHA256] != shard.Bytes {
				return Materialized{}, fmt.Errorf("object %s has conflicting declared sizes %d and %d", shard.SHA256, sizes[shard.SHA256], shard.Bytes)
			}
		} else {
			var err error
			path, err = cache.Fetch(ctx, shard.URL, shard.SHA256, shard.Bytes)
			if err != nil {
				return Materialized{}, fmt.Errorf("%s shard %s: %w", shard.Manifest, shard.SHA256[:12], err)
			}
			fetched[shard.SHA256] = path
			sizes[shard.SHA256] = shard.Bytes
		}
		result.Objects = append(result.Objects, MaterializedObject{Shard: shard, Path: path})
		if progress != nil {
			progress(MaterializeProgress{Current: position + 1, Total: len(bom.Shards), Shard: shard, Path: path})
		}
	}
	return result, nil
}
