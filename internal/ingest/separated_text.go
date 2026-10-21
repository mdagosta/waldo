// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// StreamSeparatedTextBatches maps a raw UTF-8 text stream to one canonical
// record per literal separator-delimited segment. The upstream bytes remain
// immutable evidence; only the logical record boundary is interpreted.
func StreamSeparatedTextBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("separated-text batch consumer is required")
	}
	batch := TextBatch{}
	flush := func() error {
		if len(batch.Rows) == 0 && batch.RejectedDocs == 0 {
			return nil
		}
		if err := consume(batch); err != nil {
			return err
		}
		batch = TextBatch{}
		return nil
	}
	for _, input := range plan.Inputs {
		if input.Adapter != ProfileSeparatedText {
			return fmt.Errorf("input %s requires the %s adapter, not separated-text", input.Artifact.Path, input.Adapter)
		}
		file, verified, err := openVerifiedInput(ctx, input.Artifact)
		if err != nil {
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		separator := []byte(input.Profile.Separator)
		scanner := bufio.NewScanner(&contextReader{ctx: ctx, reader: file})
		scanner.Split(splitLiteral(separator))
		scanner.Buffer(make([]byte, 64<<10), int(plan.Writer.RecordMaximumBytes)+len(separator))
		var recordNumber, consumed int64
		for scanner.Scan() {
			recordNumber++
			raw := append([]byte(nil), scanner.Bytes()...)
			consumed += int64(len(raw) + len(separator))
			text := strings.TrimSpace(string(raw))
			if text == "" {
				if input.Profile.OnEmpty == "skip" {
					batch.RejectedDocs++
					if batch.Rejections == nil {
						batch.Rejections = map[string]int64{}
					}
					batch.Rejections[RejectionEmpty]++
					continue
				}
				_ = file.Close()
				return fmt.Errorf("adapt %s: record %d: %w", input.Artifact.Path, recordNumber, errEmptyProfiledText)
			}
			if int64(len(text)) > plan.Writer.RecordMaximumBytes {
				_ = file.Close()
				return fmt.Errorf("adapt %s: record %d is %d bytes; maximum is %d bytes", input.Artifact.Path, recordNumber, len(text), plan.Writer.RecordMaximumBytes)
			}
			metadata, _ := json.Marshal(map[string]any{"source_path": input.SourcePath, "record": recordNumber, "representation": "separator-delimited-text"})
			meta := string(metadata)
			row, err := profiledFileRow(plan, input, text+"\n", fmt.Sprintf("sha256:%s#record=%d", input.Artifact.SHA256, recordNumber), "", "", "", &meta)
			if err != nil {
				if errors.Is(err, errLicensePolicy) {
					batch.RejectedDocs++
					if batch.Rejections == nil {
						batch.Rejections = map[string]int64{}
					}
					batch.Rejections[RejectionLicense]++
					continue
				}
				_ = file.Close()
				return fmt.Errorf("adapt %s: record %d: %w", input.Artifact.Path, recordNumber, err)
			}
			if len(batch.Rows) > 0 && batch.LogicalBytes+int64(len(row.Text)) > plan.Writer.AdapterBatchBytes {
				if err := flush(); err != nil {
					_ = file.Close()
					return err
				}
			}
			batch.Rows = append(batch.Rows, row)
			batch.LogicalBytes += int64(len(row.Text))
			batch.InputBytes = min(consumed, input.Artifact.Bytes)
			if batch.LogicalBytes >= plan.Writer.AdapterBatchBytes {
				if err := flush(); err != nil {
					_ = file.Close()
					return err
				}
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		if err := unchangedInput(file, verified); err != nil {
			_ = file.Close()
			return fmt.Errorf("adapt %s: %w", input.Artifact.Path, err)
		}
		if err := file.Close(); err != nil {
			return err
		}
		batch.InputBytes = input.Artifact.Bytes
	}
	return flush()
}

func splitLiteral(separator []byte) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if index := bytes.Index(data, separator); index >= 0 {
			return index + len(separator), data[:index], nil
		}
		if atEOF {
			if len(data) == 0 {
				return 0, nil, nil
			}
			return len(data), data, nil
		}
		return 0, nil, nil
	}
}
