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
)

// TextBatch is the typed boundary between text-family adapters and the
// partitioning/writer stage. LogicalBytes counts payload bytes, not Go object
// overhead or encoded Parquet bytes.
type TextBatch struct {
	Rows         []shard.TextRow
	LogicalBytes int64
	RejectedDocs int64
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
		row, size, err := readTextRow(ctx, plan, input, recordMaximum)
		if err != nil {
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
		return shard.TextRow{}, 0, fmt.Errorf("record is not NUL-free UTF-8")
	}
	var contentHash [sha256.Size]byte
	copy(contentHash[:], hasher.Sum(nil))
	sourceName := plan.Source.Name
	return shard.TextRow{
		ContentSHA256: contentHash,
		Text:          text,
		Source:        "sha256:" + input.Artifact.SHA256,
		SourceName:    &sourceName,
		License:       plan.License,
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
