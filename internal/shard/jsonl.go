// Package shard reads WALDO's native shard containers.
package shard

import (
	"fmt"
	"io"

	"github.com/openwaldo/waldo/internal/record"
	"github.com/parquet-go/parquet-go"
)

type Row struct {
	SHA256     string `parquet:"sha256"`
	Kind       string `parquet:"kind,dict"`
	Text       string `parquet:"text"`
	Source     string `parquet:"source"`
	SourceName string `parquet:"source_name,dict"`
	License    string `parquet:"license,dict"`
	LicenseRaw string `parquet:"license_raw,dict"`
	Lang       string `parquet:"lang,dict"`
	LangScore  int64  `parquet:"lang_score"`
	Date       string `parquet:"date,dict"`
	Tokens     int64  `parquet:"tokens"`
	Meta       string `parquet:"meta"`
}

type Statistics struct {
	Bytes  int64
	Docs   int64
	Tokens int64
}

// WriteJSONL converts a native Parquet shard into canonical JSONL while
// bounding application memory to a small row batch.
func WriteJSONL(dst io.Writer, src io.ReaderAt, size int64) (Statistics, error) {
	file, err := parquet.OpenFile(src, size)
	if err != nil {
		return Statistics{}, err
	}
	return writeSchemaOneJSONL(dst, file)
}

func writeSchemaOneJSONL(dst io.Writer, file *parquet.File) (Statistics, error) {
	line := make([]byte, 0, 64<<10)
	var stats Statistics
	_, err := scan(file, false, func(position int64, _ RecordView, canonical record.Record, meta string) error {
		var appendErr error
		line, appendErr = canonical.AppendCanonical(line[:0], []byte(meta))
		if appendErr != nil {
			return fmt.Errorf("record %d: %w", position, appendErr)
		}
		n, writeErr := dst.Write(line)
		stats.Bytes += int64(n)
		if writeErr != nil {
			return writeErr
		}
		stats.Docs++
		stats.Tokens += canonical.Tokens
		return nil
	})
	return stats, err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int32Value(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
