// Package shard reads WALDO's native shard containers.
package shard

import (
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

// WriteJSONL converts a native Parquet shard into canonical JSONL while
// bounding application memory to a small row batch.
func WriteJSONL(dst io.Writer, src io.ReaderAt, size int64) (int64, error) {
	file, err := parquet.OpenFile(src, size)
	if err != nil {
		return 0, err
	}
	reader := parquet.NewGenericReader[Row](file)
	defer reader.Close()
	rows := make([]Row, 512)
	line := make([]byte, 0, 64<<10)
	var written int64
	recordNumber := 0
	for {
		count, readErr := reader.Read(rows)
		for i := 0; i < count; i++ {
			line, err = rows[i].appendJSONL(line[:0])
			if err != nil {
				return written, fmt.Errorf("record %d: %w", recordNumber, err)
			}
			n, writeErr := dst.Write(line)
			written += int64(n)
			if writeErr != nil {
				return written, writeErr
			}
			recordNumber++
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
		if count == 0 {
			return written, nil
		}
	}
}
