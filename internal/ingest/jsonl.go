package ingest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/openwaldo/waldo-new/internal/shard"
)

// StreamJSONLTextBatches streams ordinary, gzip, or zstd JSONL records whose
// top-level `text` member is a string. Unknown members are ignored: WALDO owns
// the canonical output schema and intentionally projects only training text.
func StreamJSONLTextBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("JSONL batch consumer is required")
	}
	for _, input := range plan.Inputs {
		if input.Adapter != "jsonl" {
			return fmt.Errorf("input %s requires the %s adapter, not the JSONL adapter", input.Artifact.Path, input.Adapter)
		}
	}
	return streamJSONLTextBatches(ctx, plan, plan.Writer.AdapterBatchBytes, plan.Writer.RecordMaximumBytes, consume)
}

func streamJSONLTextBatches(ctx context.Context, plan Plan, batchMaximum, recordMaximum int64, consume func(TextBatch) error) error {
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
		if err := streamJSONLInput(ctx, plan, input, recordMaximum, func(row shard.TextRow, size int64) error {
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
		}); err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
	}
	return flush()
}

func streamJSONLInput(ctx context.Context, plan Plan, input PlanInput, maximum int64, emit func(shard.TextRow, int64) error) error {
	file, err := os.Open(input.Artifact.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	verified, err := verifyPlannedArtifact(ctx, file, input.Artifact)
	if err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader, err := openDecompressed(&contextReader{ctx: ctx, reader: file}, input.Artifact.Compression)
	if err != nil {
		return fmt.Errorf("open %s stream: %w", input.Artifact.Compression, err)
	}

	// JSON syntax adds only bounded overhead to the canonical text limit. The
	// scanner keeps a malformed line from forcing unbounded allocation.
	lineMaximum := maximum + 1<<20
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), int(lineMaximum))
	sourceName := plan.Source.Name
	lineNumber := int64(0)
	documents := int64(0)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			_ = reader.Close()
			return err
		}
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw struct {
			Text *string `json:"text"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			_ = reader.Close()
			return fmt.Errorf("line %d is not a JSON object with a string text field: %w", lineNumber, err)
		}
		if raw.Text == nil {
			_ = reader.Close()
			return fmt.Errorf("line %d has no string text field", lineNumber)
		}
		text := *raw.Text
		if text == "" {
			_ = reader.Close()
			return fmt.Errorf("line %d has empty text", lineNumber)
		}
		if int64(len(text)) > maximum {
			_ = reader.Close()
			return fmt.Errorf("line %d text is %d bytes; maximum is %d bytes", lineNumber, len(text), maximum)
		}
		if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
			_ = reader.Close()
			return fmt.Errorf("line %d text is not NUL-free UTF-8", lineNumber)
		}
		contentHash := sha256.Sum256([]byte(text))
		row := shard.TextRow{
			ContentSHA256: contentHash,
			Text:          text,
			Source:        fmt.Sprintf("sha256:%s#line=%d", input.Artifact.SHA256, lineNumber),
			SourceName:    &sourceName,
			License:       plan.License,
		}
		if err := emit(row, int64(len(text))); err != nil {
			_ = reader.Close()
			return err
		}
		documents++
	}
	scanErr := scanner.Err()
	closeErr := reader.Close()
	if scanErr != nil {
		return fmt.Errorf("read JSONL near line %d (maximum encoded line is %d bytes): %w", lineNumber+1, lineMaximum, scanErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if documents == 0 {
		return fmt.Errorf("JSONL input contains no documents")
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if after.Size() != verified.Size() || !after.ModTime().Equal(verified.ModTime()) {
		return fmt.Errorf("artifact changed while it was being converted")
	}
	return nil
}
