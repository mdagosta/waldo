// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/openwaldo/waldo/internal/shard"
	"github.com/parquet-go/parquet-go"
)

// StreamParquetTextBatches projects the planned flat text column from raw
// Parquet inputs and maps it directly into canonical typed batches. It never
// materializes JSONL, an untyped row map, an entire row group, or a whole input
// file in memory.
func StreamParquetTextBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("Parquet batch consumer is required")
	}
	for _, input := range plan.Inputs {
		if input.Adapter != "parquet" {
			return fmt.Errorf("input %s requires the %s adapter, not the Parquet adapter", input.Artifact.Path, input.Adapter)
		}
		if strings.Contains(input.TextColumn, ".") {
			return fmt.Errorf("input %s uses unsupported nested text column %q", input.Artifact.Path, input.TextColumn)
		}
	}
	return streamParquetTextBatches(ctx, plan, plan.Writer.AdapterBatchBytes, plan.Writer.RecordMaximumBytes, consume)
}

func streamParquetTextBatches(ctx context.Context, plan Plan, batchMaximum, recordMaximum int64, consume func(TextBatch) error) error {
	batch := TextBatch{}
	flush := func() error {
		if len(batch.Rows) == 0 {
			return nil
		}
		if err := consume(batch); err != nil {
			return err
		}
		batch = TextBatch{}
		return nil
	}
	for _, input := range plan.Inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := os.Open(input.Artifact.Path)
		if err != nil {
			return err
		}
		verified, err := verifyPlannedArtifact(ctx, file, input.Artifact)
		if err != nil {
			_ = file.Close()
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		err = readProjectedText(ctx, file, input, plan, recordMaximum, func(row shard.TextRow, size int64) error {
			if len(batch.Rows) > 0 && batch.LogicalBytes+size > batchMaximum {
				if err := flush(); err != nil {
					return err
				}
			}
			batch.Rows = append(batch.Rows, row)
			batch.LogicalBytes += size
			if batch.LogicalBytes >= batchMaximum {
				return flush()
			}
			return nil
		})
		if err == nil {
			after, statErr := file.Stat()
			if statErr != nil {
				err = statErr
			} else if after.Size() != verified.Size() || !after.ModTime().Equal(verified.ModTime()) {
				err = fmt.Errorf("artifact changed while it was being converted")
			}
		}
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return flush()
}

func readProjectedText(ctx context.Context, input *os.File, planned PlanInput, plan Plan, maximum int64, emit func(shard.TextRow, int64) error) error {
	parquetFile, err := parquet.OpenFile(input, planned.Artifact.Bytes)
	if err != nil {
		return fmt.Errorf("open Parquet: %w", err)
	}
	rowGroups := parquetFile.RowGroups()
	if len(rowGroups) == 0 || parquetFile.NumRows() == 0 {
		return fmt.Errorf("Parquet input contains no rows")
	}
	var sourceGroup parquet.RowGroup
	if len(rowGroups) == 1 {
		sourceGroup = rowGroups[0]
	} else {
		sourceGroup = parquet.MultiRowGroup(rowGroups...)
	}
	projection := parquet.NewSchema("waldo_projection", parquet.Group{
		planned.TextColumn: parquet.Optional(parquet.String()),
	})
	conversion, err := parquet.Convert(projection, parquetFile.Schema())
	if err != nil {
		return fmt.Errorf("project text column %q: %w", planned.TextColumn, err)
	}
	rows := parquet.ConvertRowGroup(sourceGroup, conversion).Rows()
	defer rows.Close()
	rowBuffer := make([]parquet.Row, 1)
	source, license, err := plan.sourceFor(planned)
	if err != nil {
		return err
	}
	sourceName := source.Name
	var rowNumber int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := rows.ReadRows(rowBuffer)
		if count > 0 {
			rowNumber++
			row := rowBuffer[0]
			if len(row) != 1 || row[0].IsNull() {
				return fmt.Errorf("row %d has a null or non-scalar %q value", rowNumber, planned.TextColumn)
			}
			textBytes := row[0].ByteArray()
			if int64(len(textBytes)) > maximum {
				return fmt.Errorf("row %d is %d bytes; maximum is %d bytes (choose an explicit splitter recipe)", rowNumber, len(textBytes), maximum)
			}
			text := string(textBytes)
			if text == "" {
				return fmt.Errorf("row %d is empty", rowNumber)
			}
			if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
				return fmt.Errorf("row %d is not NUL-free UTF-8", rowNumber)
			}
			contentHash := sha256.Sum256(textBytes)
			canonical := shard.TextRow{
				ContentSHA256: contentHash,
				Text:          text,
				Source:        fmt.Sprintf("sha256:%s#row=%d", planned.Artifact.SHA256, rowNumber),
				SourceName:    &sourceName,
				License:       license,
				MainContent:   true,
			}
			if err := emit(canonical, int64(len(textBytes))); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if rowNumber != parquetFile.NumRows() {
		return fmt.Errorf("projected row count %d does not match footer count %d", rowNumber, parquetFile.NumRows())
	}
	return nil
}

func verifyPlannedArtifact(ctx context.Context, file *os.File, artifact Artifact) (os.FileInfo, error) {
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	hasher := sha256.New()
	buffer := make([]byte, 1<<20)
	written, err := io.CopyBuffer(hasher, &contextReader{ctx: ctx, reader: file}, buffer)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if written != artifact.Bytes || hex.EncodeToString(hasher.Sum(nil)) != artifact.SHA256 || before.Size() != written || after.Size() != written || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("artifact changed after the ingestion plan was accepted")
	}
	return after, nil
}
