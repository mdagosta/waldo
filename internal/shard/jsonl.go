// Package shard reads WALDO's native shard containers.
package shard

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/openwaldo/waldo-new/internal/record"
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

func (row Row) appendJSONL(dst []byte) ([]byte, error) {
	return (record.Record{
		SHA256: row.SHA256, Kind: row.Kind, Text: row.Text, Source: row.Source,
		SourceName: row.SourceName, License: row.License, LicenseRaw: row.LicenseRaw,
		Lang: row.Lang, LangScore: row.LangScore, Date: row.Date, Tokens: row.Tokens,
	}).AppendCanonical(dst, []byte(row.Meta))
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
	if schema, ok := file.Lookup("waldo.record_schema"); ok && schema == fmt.Sprint(TextRecordSchema) {
		return writeCanonicalTextJSONL(dst, file)
	}
	reader := parquet.NewGenericReader[Row](file)
	defer reader.Close()
	rows := make([]Row, 512)
	line := make([]byte, 0, 64<<10)
	var stats Statistics
	recordNumber := 0
	for {
		count, readErr := reader.Read(rows)
		for i := 0; i < count; i++ {
			line, err = rows[i].appendJSONL(line[:0])
			if err != nil {
				return stats, fmt.Errorf("record %d: %w", recordNumber, err)
			}
			n, writeErr := dst.Write(line)
			stats.Bytes += int64(n)
			if writeErr != nil {
				return stats, writeErr
			}
			stats.Docs++
			stats.Tokens += rows[i].Tokens
			recordNumber++
		}
		if errors.Is(readErr, io.EOF) {
			return stats, nil
		}
		if readErr != nil {
			return stats, readErr
		}
		if count == 0 {
			return stats, nil
		}
	}
}

func writeCanonicalTextJSONL(dst io.Writer, file *parquet.File) (Statistics, error) {
	reader := parquet.NewGenericReader[TextRow](file)
	defer reader.Close()
	rows := make([]TextRow, 512)
	line := make([]byte, 0, 64<<10)
	var stats Statistics
	recordNumber := 0
	for {
		count, readErr := reader.Read(rows)
		for i := 0; i < count; i++ {
			row := rows[i]
			canonical := record.Record{
				SHA256: hex.EncodeToString(row.ContentSHA256[:]), Kind: record.KindPretrain,
				Text: row.Text, Source: row.Source, SourceName: stringValue(row.SourceName),
				License: row.License, LicenseRaw: stringValue(row.LicenseRaw),
				Lang: stringValue(row.Language), LangScore: int64(int32Value(row.LanguageScore)),
				Date: stringValue(row.Date), Tokens: int64Value(row.TokenCount),
			}
			meta := []byte(stringValue(row.Meta))
			var err error
			line, err = canonical.AppendCanonical(line[:0], meta)
			if err != nil {
				return stats, fmt.Errorf("record %d: %w", recordNumber, err)
			}
			n, writeErr := dst.Write(line)
			stats.Bytes += int64(n)
			if writeErr != nil {
				return stats, writeErr
			}
			stats.Docs++
			stats.Tokens += canonical.Tokens
			recordNumber++
		}
		if errors.Is(readErr, io.EOF) {
			return stats, nil
		}
		if readErr != nil {
			return stats, readErr
		}
		if count == 0 {
			return stats, nil
		}
	}
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
