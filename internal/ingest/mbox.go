// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/openwaldo/waldo/internal/shard"
)

// StreamMboxTextBatches treats mbox as a general record container. Each mbox
// envelope starts one record; the envelope itself is container framing and is
// not included in the RFC 822 message stored in the canonical row.
func StreamMboxTextBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("mbox batch consumer is required")
	}
	for _, input := range plan.Inputs {
		if input.Adapter != "mbox" {
			return fmt.Errorf("input %s requires the %s adapter, not the mbox adapter", input.Artifact.Path, input.Adapter)
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
		err := streamMboxInput(ctx, plan, input, plan.Writer.RecordMaximumBytes, func(row shard.TextRow, size int64) error {
			if len(batch.Rows) > 0 && batch.LogicalBytes+size > plan.Writer.AdapterBatchBytes {
				if err := flush(); err != nil {
					return err
				}
			}
			batch.Rows = append(batch.Rows, row)
			batch.LogicalBytes += size
			if batch.LogicalBytes >= plan.Writer.AdapterBatchBytes {
				return flush()
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
	}
	return flush()
}

func streamMboxInput(ctx context.Context, plan Plan, input PlanInput, maximum int64, emit func(shard.TextRow, int64) error) error {
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
	stream, err := openDecompressed(&contextReader{ctx: ctx, reader: file}, input.Artifact.Compression)
	if err != nil {
		return fmt.Errorf("open %s stream: %w", input.Artifact.Compression, err)
	}
	reader := bufio.NewReaderSize(stream, 64<<10)
	source, license, err := plan.sourceFor(input)
	if err != nil {
		_ = stream.Close()
		return err
	}
	sourceName := source.Name
	messageNumber := int64(0)
	started := false
	var message bytes.Buffer
	emitMessage := func() error {
		if message.Len() == 0 {
			return fmt.Errorf("message %d is empty", messageNumber+1)
		}
		text := message.String()
		if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
			return fmt.Errorf("message %d is not NUL-free UTF-8", messageNumber+1)
		}
		messageNumber++
		digest := sha256.Sum256(message.Bytes())
		var metadata *string
		if input.SourcePath != "" {
			encoded, err := json.Marshal(map[string]string{"source_path": input.SourcePath})
			if err != nil {
				return err
			}
			value := string(encoded)
			metadata = &value
		}
		row := shard.TextRow{
			ContentSHA256: digest,
			Text:          text,
			Source:        fmt.Sprintf("sha256:%s#message=%d", input.Artifact.SHA256, messageNumber),
			SourceName:    &sourceName,
			License:       license,
			Meta:          metadata,
			MainContent:   true,
		}
		if err := emit(row, int64(message.Len())); err != nil {
			return err
		}
		message.Reset()
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = stream.Close()
			return err
		}
		line, readErr := readMboxLine(reader, maximum+1)
		if len(line) > 0 {
			if bytes.HasPrefix(line, []byte("From ")) {
				if started {
					if err := emitMessage(); err != nil {
						_ = stream.Close()
						return err
					}
				}
				started = true
			} else {
				if !started {
					_ = stream.Close()
					return fmt.Errorf("content precedes the first mbox envelope")
				}
				if int64(message.Len()+len(line)) > maximum {
					_ = stream.Close()
					return fmt.Errorf("message %d exceeds %d bytes", messageNumber+1, maximum)
				}
				_, _ = message.Write(line)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				_ = stream.Close()
				return readErr
			}
			break
		}
	}
	if !started {
		_ = stream.Close()
		return fmt.Errorf("mbox contains no messages")
	}
	if err := emitMessage(); err != nil {
		_ = stream.Close()
		return err
	}
	if err := stream.Close(); err != nil {
		return err
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

func readMboxLine(reader *bufio.Reader, maximum int64) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if int64(len(line)+len(fragment)) > maximum {
			return nil, fmt.Errorf("mbox line exceeds %d bytes", maximum)
		}
		line = append(line, fragment...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}
