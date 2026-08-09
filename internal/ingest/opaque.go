// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/openwaldo/waldo/internal/shard"
)

// Base64 has long unbroken symbol runs which can make tokenizer work grow
// pathologically for multi-megabyte rows. Keep the lossless representation
// ordered and complete while bounding the cost of each canonical row.
const opaqueChunkBytes = 256 << 10

// StreamOpaqueTextBatches losslessly represents arbitrary acquired bytes in
// canonical text shards until a native adapter and shard representation exist.
func StreamOpaqueTextBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("text batch consumer is required")
	}
	for _, input := range plan.Inputs {
		if input.Adapter != "opaque-base64" {
			return fmt.Errorf("input %s requires the %s adapter, not the opaque adapter", input.Artifact.Path, input.Adapter)
		}
		if err := streamOpaqueInput(ctx, plan, input, plan.Writer.AdapterBatchBytes, consume); err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
	}
	return nil
}

func streamOpaqueInput(ctx context.Context, plan Plan, input PlanInput, batchMaximum int64, consume func(TextBatch) error) error {
	file, err := os.Open(input.Artifact.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return err
	}
	source, license, err := plan.sourceFor(input)
	if err != nil {
		return err
	}
	chunks := (input.Artifact.Bytes + opaqueChunkBytes - 1) / opaqueChunkBytes
	buffer := make([]byte, opaqueChunkBytes)
	originalHash := sha256.New()
	var readBytes int64
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
	for chunk := int64(0); chunk < chunks; chunk++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := io.ReadFull(file, buffer)
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		data := buffer[:count]
		_, _ = originalHash.Write(data)
		readBytes += int64(count)
		header := fmt.Sprintf("WALDO_OPAQUE_BINARY_V1\nmedia_type: %s\nencoding: base64\nchunk: %d/%d\n\n", input.Artifact.MediaType, chunk+1, chunks)
		text := header + base64.StdEncoding.EncodeToString(data)
		contentHash := sha256.Sum256([]byte(text))
		metadata, err := json.Marshal(map[string]any{
			"source_path": input.SourcePath, "representation": "opaque-base64", "media_type": input.Artifact.MediaType,
			"chunk": chunk + 1, "chunks": chunks,
		})
		if err != nil {
			return err
		}
		metadataText := string(metadata)
		sourceName := source.Name
		row := shard.TextRow{
			ContentSHA256: contentHash, Text: text, Source: "sha256:" + input.Artifact.SHA256,
			SourceName: &sourceName, License: license, Meta: &metadataText,
		}
		if len(batch.Rows) > 0 && batch.LogicalBytes+int64(len(text)) > batchMaximum {
			if err := flush(); err != nil {
				return err
			}
		}
		batch.Rows = append(batch.Rows, row)
		batch.LogicalBytes += int64(len(text))
		if batch.LogicalBytes >= batchMaximum {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if readBytes != input.Artifact.Bytes || hex.EncodeToString(originalHash.Sum(nil)) != input.Artifact.SHA256 || before.Size() != readBytes || after.Size() != readBytes || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("artifact changed after the ingestion plan was accepted")
	}
	return nil
}
