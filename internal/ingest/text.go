// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/openwaldo/waldo/internal/shard"
)

// TextBatch is the typed boundary between text-family adapters and the
// partitioning/writer stage. LogicalBytes counts payload bytes, not Go object
// overhead or encoded Parquet bytes.
type TextBatch struct {
	Rows         []shard.TextRow
	LogicalBytes int64
	RejectedDocs int64
	Rejections   map[string]int64
}

const (
	RejectionEmpty     = "empty"
	RejectionMalformed = "malformed"
	RejectionMapping   = "mapping"
	RejectionLicense   = "license-policy"
)

var errTextRequiresOpaqueFallback = errors.New("text input requires lossless opaque fallback")

func rejectionBatch(reason string) TextBatch {
	return TextBatch{RejectedDocs: 1, Rejections: map[string]int64{reason: 1}}
}

// StreamTextBatches preserves one text or Markdown file as one logical row.
// It revalidates every planned artifact while reading and retains at most one
// configured batch plus one bounded record. No normalization, splitting,
// replacement, or Markdown rendering is implicit.
func StreamTextBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("text batch consumer is required")
	}
	return streamTextBatches(ctx, plan, plan.Writer.AdapterBatchBytes, plan.Writer.RecordMaximumBytes, consume)
}

func streamTextBatches(ctx context.Context, plan Plan, batchMaximum, recordMaximum int64, consume func(TextBatch) error) error {
	if batchMaximum <= 0 || recordMaximum <= 0 {
		return fmt.Errorf("text adapter limits must be positive")
	}
	for _, input := range plan.Inputs {
		if input.Adapter != "text" && input.Adapter != "markdown" {
			return fmt.Errorf("input %s requires the %s adapter, not the text adapter", input.Artifact.Path, input.Adapter)
		}
	}
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
		if input.Artifact.Bytes > recordMaximum {
			if err := flush(); err != nil {
				return err
			}
			valid, err := isNULFreeUTF8File(ctx, input.Artifact.Path)
			if err != nil {
				return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
			}
			if !valid {
				return fmt.Errorf("adapt %s: selected text adapter requires NUL-free UTF-8", input.Artifact.Path)
			}
			if err := streamLargeTextInput(ctx, plan, input, min(batchMaximum, recordMaximum), consume); err != nil {
				return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
			}
			continue
		}
		row, size, err := readTextRow(ctx, plan, input, recordMaximum)
		if err != nil {
			if errors.Is(err, errTextRequiresOpaqueFallback) {
				return fmt.Errorf("adapt %s: selected text adapter requires NUL-free UTF-8", input.Artifact.Path)
			}
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		if len(batch.Rows) > 0 && batch.LogicalBytes+size > batchMaximum {
			if err := flush(); err != nil {
				return err
			}
		}
		batch.Rows = append(batch.Rows, row)
		batch.LogicalBytes += size
		if batch.LogicalBytes >= batchMaximum {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func isNULFreeUTF8File(ctx context.Context, path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	buffer := make([]byte, (1<<20)+utf8.UTFMax)
	carried := 0
	for {
		count, readErr := file.Read(buffer[carried:])
		data := buffer[:carried+count]
		if bytes.IndexByte(data, 0) >= 0 {
			return false, nil
		}
		if readErr == io.EOF {
			return utf8.Valid(data), nil
		}
		if readErr != nil {
			return false, readErr
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		cut := len(data)
		for trim := 0; trim < utf8.UTFMax && cut >= 0 && !utf8.Valid(data[:cut]); trim++ {
			cut--
		}
		if cut < 0 || !utf8.Valid(data[:cut]) {
			return false, nil
		}
		carried = copy(buffer, data[cut:])
	}
}

func streamLargeTextInput(ctx context.Context, plan Plan, input PlanInput, chunkMaximum int64, consume func(TextBatch) error) error {
	if chunkMaximum <= 0 || chunkMaximum > int64(^uint(0)>>1) {
		return fmt.Errorf("text chunk limit is invalid")
	}
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
	chunks := (input.Artifact.Bytes + chunkMaximum - 1) / chunkMaximum
	hasher := sha256.New()
	reader := &contextReader{ctx: ctx, reader: io.TeeReader(file, hasher)}
	carry := make([]byte, 0, utf8.UTFMax)
	buffer := make([]byte, int(chunkMaximum))
	var readBytes int64
	for chunk := int64(0); chunk < chunks; chunk++ {
		data := buffer[:len(carry)]
		copy(data, carry)
		carry = carry[:0]
		needed := int(chunkMaximum) - len(data)
		count, readErr := io.ReadFull(reader, buffer[len(data):len(data)+needed])
		data = buffer[:len(data)+count]
		readBytes += int64(count)
		final := readErr == io.EOF || readErr == io.ErrUnexpectedEOF
		if readErr != nil && !final {
			return readErr
		}
		cut := len(data)
		if !final {
			for trim := 0; trim < utf8.UTFMax && cut > 0 && !utf8.Valid(data[:cut]); trim++ {
				cut--
			}
			if !utf8.Valid(data[:cut]) {
				return fmt.Errorf("record is not UTF-8")
			}
			carry = append(carry, data[cut:]...)
			data = data[:cut]
		} else if !utf8.Valid(data) {
			return fmt.Errorf("record is not UTF-8")
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return fmt.Errorf("record is not NUL-free UTF-8")
		}
		text := string(data)
		contentHash := sha256.Sum256(data)
		metadata, err := json.Marshal(map[string]any{
			"source_path": input.SourcePath, "representation": "text-chunk", "chunk": chunk + 1, "chunks": chunks,
		})
		if err != nil {
			return err
		}
		metadataText := string(metadata)
		sourceName := source.Name
		if err := consume(TextBatch{Rows: []shard.TextRow{{
			ContentSHA256: contentHash, Text: text, Source: "sha256:" + input.Artifact.SHA256,
			SourceName: &sourceName, License: license, Meta: &metadataText, MainContent: true,
		}}, LogicalBytes: int64(len(data))}); err != nil {
			return err
		}
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if len(carry) != 0 || readBytes != input.Artifact.Bytes || hex.EncodeToString(hasher.Sum(nil)) != input.Artifact.SHA256 || before.Size() != readBytes || after.Size() != readBytes || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("artifact changed after the ingestion plan was accepted")
	}
	return nil
}

func readTextRow(ctx context.Context, plan Plan, input PlanInput, maximum int64) (shard.TextRow, int64, error) {
	file, err := os.Open(input.Artifact.Path)
	if err != nil {
		return shard.TextRow{}, 0, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return shard.TextRow{}, 0, err
	}
	if before.Size() > maximum {
		return shard.TextRow{}, 0, fmt.Errorf("record is %d bytes; maximum is %d bytes (choose an explicit splitter recipe)", before.Size(), maximum)
	}

	var content strings.Builder
	content.Grow(int(before.Size()))
	hasher := sha256.New()
	reader := &contextReader{ctx: ctx, reader: io.LimitReader(file, maximum+1)}
	written, err := io.Copy(io.MultiWriter(&content, hasher), reader)
	if err != nil {
		return shard.TextRow{}, 0, err
	}
	if written > maximum {
		return shard.TextRow{}, 0, fmt.Errorf("record exceeds %d bytes (choose an explicit splitter recipe)", maximum)
	}
	after, err := file.Stat()
	if err != nil {
		return shard.TextRow{}, 0, err
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if written != input.Artifact.Bytes || digest != input.Artifact.SHA256 || before.Size() != written || after.Size() != written || !before.ModTime().Equal(after.ModTime()) {
		return shard.TextRow{}, 0, fmt.Errorf("artifact changed after the ingestion plan was accepted")
	}
	text := content.String()
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return shard.TextRow{}, 0, fmt.Errorf("%w: record is not NUL-free UTF-8", errTextRequiresOpaqueFallback)
	}
	var contentHash [sha256.Size]byte
	copy(contentHash[:], hasher.Sum(nil))
	source, license, err := plan.sourceFor(input)
	if err != nil {
		return shard.TextRow{}, 0, err
	}
	sourceName := source.Name
	var metadata *string
	if input.SourcePath != "" {
		encoded, err := json.Marshal(map[string]string{"source_path": input.SourcePath})
		if err != nil {
			return shard.TextRow{}, 0, fmt.Errorf("encode source path metadata: %w", err)
		}
		value := string(encoded)
		metadata = &value
	}
	return shard.TextRow{
		ContentSHA256: contentHash,
		Text:          text,
		Source:        "sha256:" + input.Artifact.SHA256,
		SourceName:    &sourceName,
		License:       license,
		Meta:          metadata,
		MainContent:   true,
	}, written, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
