package ingest

import (
	"fmt"

	"github.com/openwaldo/waldo/internal/shard"
	"go.etcd.io/bbolt"
)

var dedupBucket = []byte("content-sha256")

type deduplicator struct {
	database *bbolt.DB
	input    int64
	kept     int64
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
	result := TextBatch{Rows: make([]shard.TextRow, 0, len(batch.Rows))}
	err := dedup.database.Update(func(transaction *bbolt.Tx) error {
		bucket := transaction.Bucket(dedupBucket)
		if bucket == nil {
			return fmt.Errorf("deduplication bucket is missing")
		}
		for _, row := range batch.Rows {
			dedup.input++
			key := row.ContentSHA256[:]
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
