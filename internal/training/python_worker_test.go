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
	"path/filepath"
	"strings"
	"syscall"
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

func TestWorkerCommandFailsWhenOrphanHoldsOutputStream(t *testing.T) {
	previous := workerExitDrain
	workerExitDrain = 300 * time.Millisecond
	defer func() { workerExitDrain = previous }()
	worker := filepath.Join(t.TempDir(), "fake-python")
	script := `#!/bin/sh
sleep 30 &
while IFS= read -r line; do :; done
exit 0
`
	if err := os.WriteFile(worker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(worker)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	done := make(chan error, 1)
	go func() {
		_, runErr := runWorkerCommand(context.Background(), "TorchTitan", command, Request{
			ArtifactDirectory: t.TempDir(), ArtifactPrefix: "artifacts", Parameters: parameters,
			Records: recordSourceFunc(func(_ context.Context, consume func(Record) error) error {
				return consume(Record{ID: "one", Text: "hello"})
			}),
		})
		done <- runErr
	}()
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "held its output stream open") {
			t.Fatalf("orphan error = %v", runErr)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("runWorkerCommand hung after the worker exited with an orphan holding stdout")
	}
}

func TestWorkerCommandFailsWhenOrphanHoldsPipesAndWorkerNeverReadsInput(t *testing.T) {
	previous := workerExitDrain
	workerExitDrain = 300 * time.Millisecond
	defer func() { workerExitDrain = previous }()
	worker := filepath.Join(t.TempDir(), "fake-python")
	script := `#!/bin/sh
sleep 30 &
exit 0
`
	if err := os.WriteFile(worker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	parameters, err := ResolveParameters(Parameters{Steps: 100, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(worker)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	done := make(chan error, 1)
	go func() {
		_, runErr := runWorkerCommand(context.Background(), "TorchTitan", command, Request{
			ArtifactDirectory: t.TempDir(), ArtifactPrefix: "artifacts", Parameters: parameters,
			Records: recordSourceFunc(func(_ context.Context, consume func(Record) error) error {
				for index := 0; index < 20000; index++ {
					if err := consume(Record{ID: fmt.Sprintf("record-%d", index), Text: strings.Repeat("harbor lanterns ", 8)}); err != nil {
						return err
					}
				}
				return nil
			}),
		})
		done <- runErr
	}()
	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("expected a failure when the worker exits without draining input")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("runWorkerCommand hung writing records to a worker that exited")
	}
}

func TestWorkerCommandToleratesForeignStdoutLines(t *testing.T) {
	worker := filepath.Join(t.TempDir(), "fake-python")
	script := `#!/bin/sh
while IFS= read -r line; do :; done
printf '%s\n' 'NOTE: Redirects are currently not supported in Windows or MacOs.'
printf '%s\n' 'spark-1:42:99 [0] NCCL WARN Connect to 10.10.10.13<45191> failed : Connection refused'
printf '%s\n' '{"kind":"event","schema":1,"event":{"kind":"progress","message":"step 1","step":1,"tokens":2}}'
printf '%s\n' 'W0813 12:00:00.000000 42 torch/distributed/elastic/agent.py:1 some warning'
printf '%s\n' '{"kind":"complete","schema":1,"observation":{"simulated":false,"steps":1,"consumed_tokens":2,"final_loss":1.0,"artifacts":[]}}'
`
	if err := os.WriteFile(worker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	reported := false
	observation, err := runWorkerCommand(context.Background(), "TorchTitan", exec.Command(worker), Request{
		ArtifactDirectory: t.TempDir(), ArtifactPrefix: "artifacts", Parameters: parameters,
		Records: recordSourceFunc(func(_ context.Context, consume func(Record) error) error {
			return consume(Record{ID: "one", Text: "hello"})
		}),
		Report: func(event Event) { reported = true },
	})
	if err != nil {
		t.Fatalf("foreign stdout lines must not fail the run: %v", err)
	}
	if observation.Simulated || observation.Steps != 1 || !reported {
		t.Fatalf("observation = %+v, reported = %v", observation, reported)
	}
}

func TestWorkerCommandKeepsCompletionWhenWorkerExitsImmediately(t *testing.T) {
	worker := filepath.Join(t.TempDir(), "fake-python")
	script := `#!/bin/sh
while IFS= read -r line; do :; done
printf '%s\n' '{"kind":"event","schema":1,"event":{"kind":"progress","message":"step 1","step":1,"tokens":2}}'
printf '%s\n' '{"kind":"complete","schema":1,"observation":{"simulated":false,"steps":1,"consumed_tokens":2,"final_loss":1.0,"artifacts":[]}}'
exit 0
`
	if err := os.WriteFile(worker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 40; attempt++ {
		observation, err := runWorkerCommand(context.Background(), "MLX", exec.Command(worker), Request{
			ArtifactDirectory: t.TempDir(), ArtifactPrefix: "artifacts", Parameters: parameters,
			Records: recordSourceFunc(func(_ context.Context, consume func(Record) error) error {
				return consume(Record{ID: "one", Text: "hello"})
			}),
		})
		if err != nil {
			t.Fatalf("attempt %d: worker that exits right after completing must not fail: %v", attempt, err)
		}
		if observation.Steps != 1 {
			t.Fatalf("attempt %d: observation = %+v", attempt, observation)
		}
	}
}
