// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

func seedPlan(t *testing.T, root, rendezvousID string, plan model.MultiNodePlan) {
	t.Helper()
	path := model.MultiNodePlanPath(root, rendezvousID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
}

func TestAwaitMultiNodePlanReadsPublishedPlan(t *testing.T) {
	root := t.TempDir()
	seedPlan(t, root, "run-42", model.MultiNodePlan{
		Kind: model.MultiNodePlanKind, Schema: model.MultiNodePlanSchema,
		RunID: "run0001", Stage: "train-0001", Objective: "causal-language-modeling",
	})
	plan, err := awaitMultiNodePlan(context.Background(), root, "run-42", time.Minute, io.Discard)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if plan.RunID != "run0001" || plan.Stage != "train-0001" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestAwaitMultiNodePlanRejectsUnsupportedSchema(t *testing.T) {
	root := t.TempDir()
	seedPlan(t, root, "run-42", model.MultiNodePlan{Kind: model.MultiNodePlanKind, Schema: 999})
	if _, err := awaitMultiNodePlan(context.Background(), root, "run-42", time.Minute, io.Discard); err == nil || !strings.Contains(err.Error(), "has schema 999; this build supports 1") {
		t.Fatalf("schema guard error = %v", err)
	}
}

func TestAwaitMultiNodePlanRejectsWrongKind(t *testing.T) {
	root := t.TempDir()
	seedPlan(t, root, "run-42", model.MultiNodePlan{Kind: "openwaldo-bom", Schema: model.MultiNodePlanSchema})
	if _, err := awaitMultiNodePlan(context.Background(), root, "run-42", time.Minute, io.Discard); err == nil || !strings.Contains(err.Error(), `kind "openwaldo-bom"`) {
		t.Fatalf("kind guard error = %v", err)
	}
}

func TestAwaitMultiNodePlanHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := awaitMultiNodePlan(ctx, t.TempDir(), "run-42", time.Minute, io.Discard); err == nil {
		t.Fatal("expected cancellation error for an unpublished plan")
	}
}

func TestEvaluationSetValueZeroWhenAbsent(t *testing.T) {
	if got := model.EvaluationSetValue(nil); got != (training.EvaluationSet{}) {
		t.Fatalf("nil evaluation set = %+v", got)
	}
	value := &training.EvaluationSet{SHA256: strings.Repeat("a", 64), Records: 5}
	if got := model.EvaluationSetValue(value); got.SHA256 != value.SHA256 || got.Records != 5 {
		t.Fatalf("evaluation set = %+v", got)
	}
}
