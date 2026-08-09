// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"context"
	"errors"
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
