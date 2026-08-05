package training

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

const WorkerProtocolSchema = 1

type WorkerBegin struct {
	RunID              string             `json:"run_id"`
	Stage              string             `json:"stage"`
	Objective          string             `json:"objective"`
	ArchitectureSHA256 string             `json:"architecture_sha256"`
	Architecture       json.RawMessage    `json:"architecture"`
	Parameters         ResolvedParameters `json:"parameters"`
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

func WriteWorkerInput(ctx context.Context, output io.Writer, begin WorkerBegin, records RecordSource) error {
	if records == nil {
		return fmt.Errorf("worker input requires a record source")
	}
	encoder := json.NewEncoder(output)
	if err := encoder.Encode(WorkerInputFrame{Kind: "begin", Schema: WorkerProtocolSchema, Begin: &begin}); err != nil {
		return err
	}
	if err := records.Stream(ctx, func(record Record) error {
		return encoder.Encode(WorkerInputFrame{Kind: "record", Schema: WorkerProtocolSchema, Record: &record})
	}); err != nil {
		return err
	}
	return encoder.Encode(WorkerInputFrame{Kind: "end", Schema: WorkerProtocolSchema})
}

func ReadWorkerOutput(input io.Reader, consume func(WorkerOutputFrame) error) error {
	if consume == nil {
		return fmt.Errorf("worker output consumer is required")
	}
	scanner := bufio.NewScanner(input)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 16*1024*1024)
	for scanner.Scan() {
		var frame WorkerOutputFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
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
	if event.Step < 0 || event.Tokens < 0 || event.TokensPerSecond < 0 || event.ETASeconds < 0 {
		return fmt.Errorf("worker event %q contains negative progress", event.Kind)
	}
	if event.Loss != nil && (*event.Loss < 0 || math.IsNaN(*event.Loss) || math.IsInf(*event.Loss, 0)) {
		return fmt.Errorf("worker event %q contains invalid loss", event.Kind)
	}
	if math.IsNaN(event.TokensPerSecond) || math.IsInf(event.TokensPerSecond, 0) {
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
