// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

const WorkerProtocolSchema = 1

type WorkerBegin struct {
	RunID              string                `json:"run_id"`
	Stage              string                `json:"stage"`
	Objective          string                `json:"objective"`
	ArchitectureSHA256 string                `json:"architecture_sha256"`
	Architecture       json.RawMessage       `json:"architecture"`
	Parameters         ResolvedParameters    `json:"parameters"`
	Tokenizer          TokenizerSpec         `json:"tokenizer"`
	EvaluationSet      EvaluationSet         `json:"evaluation_set"`
	Initialization     *WorkerInitialization `json:"initialization,omitempty"`
	Resume             *WorkerResume         `json:"resume,omitempty"`
}

type tokenizedRecordSource struct {
	source    RecordSource
	codec     TokenCodec
	objective string
}

func (source tokenizedRecordSource) Stream(ctx context.Context, consume func(Record) error) error {
	return source.source.Stream(ctx, func(record Record) error {
		if source.objective == "assistant-response-modeling" {
			var err error
			record.Tokens, record.LossMask, err = tokenizeAssistantResponses(record.Text, source.codec)
			if err != nil {
				return fmt.Errorf("tokenize assistant response record %s: %w", record.ID, err)
			}
		} else {
			record.Tokens = source.codec.Encode(record.Text)
		}
		record.Text = ""
		return consume(record)
	})
}

func tokenizeAssistantResponses(value string, codec TokenCodec) ([]int, []bool, error) {
	roles := []string{"System", "User", "Assistant", "Tool"}
	findRole := func(offset int, first bool) (int, string, int) {
		best, role, prefix := -1, "", 0
		for _, candidate := range roles {
			marker := candidate + ": "
			if !first {
				marker = "\n\n" + marker
			}
			if position := strings.Index(value[offset:], marker); position >= 0 && (best < 0 || position < best) {
				best, role, prefix = position, candidate, len(marker)
			}
		}
		return best, role, prefix
	}
	position, role, prefix := findRole(0, true)
	if position != 0 {
		return nil, nil, fmt.Errorf("record does not begin with a supported chat role")
	}
	var tokens []int
	var mask []bool
	for {
		markerTokens := codec.Encode(value[position : position+prefix])
		tokens = append(tokens, markerTokens...)
		mask = append(mask, make([]bool, len(markerTokens))...)
		contentStart := position + prefix
		next, nextRole, nextPrefix := findRole(contentStart, false)
		contentEnd := len(value)
		if next >= 0 {
			contentEnd = contentStart + next
		}
		contentTokens := codec.Encode(value[contentStart:contentEnd])
		tokens = append(tokens, contentTokens...)
		mask = append(mask, make([]bool, len(contentTokens))...)
		if role == "Assistant" {
			for index := len(mask) - len(contentTokens); index < len(mask); index++ {
				mask[index] = true
			}
		}
		if next < 0 {
			mask = append(mask, role == "Assistant") // EOS target.
			break
		}
		position, role, prefix = contentEnd, nextRole, nextPrefix
	}
	if !slices.Contains(mask, true) {
		return nil, nil, fmt.Errorf("record contains no assistant response targets")
	}
	return tokens, mask, nil
}

type WorkerInitialization struct {
	SourceType  string   `json:"source_type,omitempty"`
	SourceID    string   `json:"source_id,omitempty"`
	SourceRunID string   `json:"source_run_id,omitempty"`
	Artifact    Artifact `json:"artifact"`
	Path        string   `json:"path"`
}

type WorkerResume struct {
	Step       int64      `json:"step"`
	Tokens     int64      `json:"tokens"`
	Checkpoint Checkpoint `json:"checkpoint"`
	Paths      []string   `json:"paths"`
}

type WorkerInputFrame struct {
	Kind   string       `json:"kind"`
	Schema int          `json:"schema"`
	Begin  *WorkerBegin `json:"begin,omitempty"`
	Record *Record      `json:"record,omitempty"`
}

type WorkerOutputFrame struct {
	Kind        string       `json:"kind"`
	Schema      int          `json:"schema"`
	Event       *Event       `json:"event,omitempty"`
	Observation *Observation `json:"observation,omitempty"`
	Error       string       `json:"error,omitempty"`
}

func WriteWorkerInput(ctx context.Context, output io.Writer, begin WorkerBegin, records, evaluationRecords RecordSource) error {
	return writeWorkerInputUntil(ctx, output, begin, records, evaluationRecords, nil)
}

var errWorkerReachedTarget = errors.New("worker reached target steps")

func writeWorkerInputUntil(ctx context.Context, output io.Writer, begin WorkerBegin, records, evaluationRecords RecordSource, stopRecords <-chan struct{}) error {
	if records == nil {
		return fmt.Errorf("worker input requires a record source")
	}
	encoder := json.NewEncoder(output)
	if err := encoder.Encode(WorkerInputFrame{Kind: "begin", Schema: WorkerProtocolSchema, Begin: &begin}); err != nil {
		return err
	}
	if evaluationRecords != nil {
		if err := evaluationRecords.Stream(ctx, func(record Record) error {
			return encoder.Encode(WorkerInputFrame{Kind: "evaluation_record", Schema: WorkerProtocolSchema, Record: &record})
		}); err != nil {
			return err
		}
	}
	if err := records.Stream(ctx, func(record Record) error {
		if stopRecords != nil {
			select {
			case <-stopRecords:
				return errWorkerReachedTarget
			default:
			}
		}
		return encoder.Encode(WorkerInputFrame{Kind: "record", Schema: WorkerProtocolSchema, Record: &record})
	}); err != nil && !errors.Is(err, errWorkerReachedTarget) {
		return err
	}
	return encoder.Encode(WorkerInputFrame{Kind: "end", Schema: WorkerProtocolSchema})
}

func ReadWorkerOutput(input io.Reader, consume func(WorkerOutputFrame) error) error {
	return ReadWorkerOutputWithSkipped(input, io.Discard, consume)
}

func ReadWorkerOutputWithSkipped(input io.Reader, skipped io.Writer, consume func(WorkerOutputFrame) error) error {
	if consume == nil {
		return fmt.Errorf("worker output consumer is required")
	}
	scanner := bufio.NewScanner(input)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 16*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if start := bytes.IndexByte(line, '{'); start < 0 {
			if len(line) > 0 {
				fmt.Fprintf(skipped, "%s\n", line)
			}
			continue
		} else if start > 0 {
			fmt.Fprintf(skipped, "%s\n", line[:start])
			line = line[start:]
		}
		var frame WorkerOutputFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("decode worker output: %w", err)
		}
		if err := frame.Validate(); err != nil {
			return err
		}
		if err := consume(frame); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (frame WorkerOutputFrame) Validate() error {
	if frame.Schema != WorkerProtocolSchema {
		return fmt.Errorf("unsupported worker protocol schema %d", frame.Schema)
	}
	payloads := 0
	if frame.Event != nil {
		payloads++
	}
	if frame.Observation != nil {
		payloads++
	}
	if frame.Error != "" {
		payloads++
	}
	if payloads != 1 {
		return fmt.Errorf("worker output %q must contain exactly one payload", frame.Kind)
	}
	switch frame.Kind {
	case "event":
		if frame.Event == nil {
			return fmt.Errorf("worker event frame is missing event")
		}
		if err := frame.Event.Validate(); err != nil {
			return err
		}
	case "complete":
		if frame.Observation == nil {
			return fmt.Errorf("worker complete frame is missing observation")
		}
	case "error":
		if frame.Error == "" {
			return fmt.Errorf("worker error frame is missing error")
		}
	default:
		return fmt.Errorf("unsupported worker output kind %q", frame.Kind)
	}
	return nil
}

func (event Event) Validate() error {
	if event.Step < 0 || event.Tokens < 0 || event.LearningRate < 0 || event.TokensPerSecond < 0 || event.ETASeconds < 0 {
		return fmt.Errorf("worker event %q contains negative progress", event.Kind)
	}
	if event.Loss != nil && (*event.Loss < 0 || math.IsNaN(*event.Loss) || math.IsInf(*event.Loss, 0)) {
		return fmt.Errorf("worker event %q contains invalid loss", event.Kind)
	}
	if math.IsNaN(event.LearningRate) || math.IsInf(event.LearningRate, 0) || math.IsNaN(event.TokensPerSecond) || math.IsInf(event.TokensPerSecond, 0) {
		return fmt.Errorf("worker event %q contains invalid throughput", event.Kind)
	}
	switch event.Kind {
	case "progress", "log":
		if event.Checkpoint != nil || event.Evaluation != nil {
			return fmt.Errorf("worker event %q contains a typed payload", event.Kind)
		}
	case "checkpoint":
		if event.Checkpoint == nil || event.Evaluation != nil {
			return fmt.Errorf("worker checkpoint event has an invalid payload")
		}
	case "evaluation":
		if event.Evaluation == nil || event.Checkpoint != nil {
			return fmt.Errorf("worker evaluation event has an invalid payload")
		}
	default:
		return fmt.Errorf("unsupported worker event kind %q", event.Kind)
	}
	return nil
}
