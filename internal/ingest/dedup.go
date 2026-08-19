// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"encoding/hex"
	"fmt"

	"github.com/openwaldo/waldo/internal/shard"
	"go.etcd.io/bbolt"
)

var dedupBucket = []byte("content-sha256")

type DedupIdentity struct {
	SHA256  string
	License string
}

type deduplicator struct {
	database *bbolt.DB
	input    int64
	kept     int64
	rejected int64
	reasons  map[string]int64
}

func (dedup *deduplicator) seedIDs(values []DedupIdentity) error {
	return dedup.database.Update(func(transaction *bbolt.Tx) error {
		bucket := transaction.Bucket(dedupBucket)
		if bucket == nil {
			return fmt.Errorf("deduplication bucket is missing")
		}
		for _, value := range values {
			key, err := dedupKey(value.SHA256, value.License)
			if err != nil {
				return err
			}
			if err := bucket.Put(key, []byte{1}); err != nil {
				return err
			}
		}
		return nil
	})
}

func openDeduplicator(path string) (*deduplicator, error) {
	database, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, err
	}
	if err := database.Update(func(transaction *bbolt.Tx) error {
		_, err := transaction.CreateBucketIfNotExists(dedupBucket)
		return err
	}); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &deduplicator{database: database}, nil
}

func (dedup *deduplicator) filter(batch TextBatch) (TextBatch, error) {
	result := TextBatch{
		Rows: make([]shard.TextRow, 0, len(batch.Rows)), RejectedDocs: batch.RejectedDocs, Rejections: batch.Rejections,
		InputBytes: batch.InputBytes, ProgressBytes: batch.ProgressBytes, ProgressTotalBytes: batch.ProgressTotalBytes,
		ProgressFiles: batch.ProgressFiles, ProgressTotalFiles: batch.ProgressTotalFiles,
	}
	dedup.rejected += batch.RejectedDocs
	if dedup.reasons == nil {
		dedup.reasons = make(map[string]int64)
	}
	for reason, count := range batch.Rejections {
		dedup.reasons[reason] += count
	}
	err := dedup.database.Update(func(transaction *bbolt.Tx) error {
		bucket := transaction.Bucket(dedupBucket)
		if bucket == nil {
			return fmt.Errorf("deduplication bucket is missing")
		}
		for _, row := range batch.Rows {
			dedup.input++
			key := append(append([]byte(nil), row.ContentSHA256[:]...), 0)
			key = append(key, row.License...)
			if bucket.Get(key) != nil {
				continue
			}
			if err := bucket.Put(key, []byte{1}); err != nil {
				return err
			}
			dedup.kept++
			result.Rows = append(result.Rows, row)
			result.LogicalBytes += int64(len(row.Text))
		}
		return nil
	})
	return result, err
}

func dedupKey(digest, license string) ([]byte, error) {
	hash, err := hex.DecodeString(digest)
	if err != nil || len(hash) != 32 {
		return nil, fmt.Errorf("invalid seeded content SHA-256 %q", digest)
	}
	if license == "" {
		return nil, fmt.Errorf("seeded content %s has no effective license", digest)
	}
	key := append(hash, 0)
	return append(key, license...), nil
}
