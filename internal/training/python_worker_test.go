// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWorkerCancellationRemainsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestWorkerCancellationHelper")
	command.Env = append(os.Environ(), "WALDO_WORKER_CANCELLATION_HELPER=1")
	_, err := runWorkerCommand(ctx, "test", command, Request{
		ArtifactDirectory: t.TempDir(),
		Records: recordSourceFunc(func(_ context.Context, consume func(Record) error) error {
			return consume(Record{ID: "one", Text: "hello"})
		}),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("worker cancellation error = %v", err)
	}
}

func TestWorkerCancellationHelper(t *testing.T) {
	if os.Getenv("WALDO_WORKER_CANCELLATION_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	time.Sleep(time.Hour)
}

func TestWorkerTargetStopsUpstreamRecordStream(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestWorkerTargetHelper")
	command.Env = append(os.Environ(), "WALDO_WORKER_TARGET_HELPER=1")
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	reported := make(chan struct{})
	streamed := 0
	observation, err := runWorkerCommand(context.Background(), "test", command, Request{
		RunID: "run", Stage: "pretrain", Objective: "causal-language-modeling",
		Parameters: parameters, ArtifactDirectory: t.TempDir(),
		Records: recordSourceFunc(func(_ context.Context, consume func(Record) error) error {
			if err := consume(Record{ID: "first", Text: "hello"}); err != nil {
				return err
			}
			streamed++
			<-reported
			for position := 1; position < 100; position++ {
				if err := consume(Record{ID: fmt.Sprintf("record-%d", position), Text: "unused"}); err != nil {
					return err
				}
				streamed++
			}
			return nil
		}),
		Report: func(event Event) {
			if event.Step == 1 {
				close(reported)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observation.Steps != 1 || streamed != 1 {
		t.Fatalf("observation steps/streamed records = %d/%d, want 1/1", observation.Steps, streamed)
	}
}

func TestWorkerTargetHelper(t *testing.T) {
	if os.Getenv("WALDO_WORKER_TARGET_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	reported := false
	for scanner.Scan() {
		var frame WorkerInputFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			os.Exit(2)
		}
		if frame.Kind == "record" && !reported {
			reported = true
			fmt.Println(`{"kind":"event","schema":1,"event":{"kind":"progress","message":"target reached","step":1,"tokens":8}}`)
		}
		if frame.Kind == "end" {
			fmt.Println(`{"kind":"complete","schema":1,"observation":{"simulated":false,"steps":1,"consumed_tokens":8,"artifacts":[]}}`)
			os.Exit(0)
		}
	}
	os.Exit(3)
}
