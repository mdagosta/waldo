// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

func TestResolveArtifactsVerifiesCurrentRealRun(t *testing.T) {
	root := t.TempDir()
	artifacts := make([]model.ModelBOMArtifact, 0, 3)
	for _, item := range []struct{ role, name string }{{"weights", "model.safetensors"}, {"configuration", "config.json"}, {"tokenizer", "tokenizer.json"}} {
		logical := "runs/0001-train-real/artifacts/" + item.name
		path := filepath.Join(root, filepath.FromSlash(logical))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte(item.role)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		artifacts = append(artifacts, model.ModelBOMArtifact{Role: item.role, Path: logical, SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))})
	}
	inspection := model.Inspection{Path: root, Model: model.ModelRecord{Name: "example", Architecture: model.Architecture{ContextTokens: 512}}, BOM: model.ModelBOM{
		CurrentRunID: "real", Runs: []model.ModelBOMRun{{ID: "fake", State: model.RunComplete, Simulated: true}, {ID: "real", State: model.RunComplete, Backend: training.Identity{Name: "mlx", Revision: "v1"}, Artifacts: artifacts}},
	}}
	resolved, err := ResolveArtifacts(inspection)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RunID != "real" || resolved.ContextTokens != 512 || !strings.HasSuffix(resolved.Weights, "model.safetensors") {
		t.Fatalf("resolved = %+v", resolved)
	}
	if err := os.WriteFile(resolved.Weights, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveArtifacts(inspection); err == nil || !strings.Contains(err.Error(), "BOM requires") {
		t.Fatalf("corruption error = %v", err)
	}
	inspection.BOM.CurrentRunID = ""
	if _, err := ResolveArtifacts(inspection); err == nil || !strings.Contains(err.Error(), "no complete non-simulated") {
		t.Fatalf("simulation error = %v", err)
	}
}

func TestMLXSessionConsumesStreamingProtocol(t *testing.T) {
	python := filepath.Join(t.TempDir(), "fake-python")
	script := `#!/bin/sh
printf '%s\n' '{"kind":"ready","schema":1,"context_tokens":512}'
while IFS= read -r line; do
  printf '%s\n' '{"kind":"token","schema":1,"data":"QQ=="}'
  printf '%s\n' '{"kind":"complete","schema":1,"tokens":1,"finish_reason":"eos","duration_ms":4}'
done
`
	if err := os.WriteFile(python, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	session, contextTokens, err := startMLXSession(context.Background(), python, Artifacts{ContextTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var streamed string
	result, err := session.Generate(context.Background(), "hello", Options{MaxTokens: 1, Temperature: 0, TopP: 1}, func(token Token) error {
		streamed += string(token.Bytes)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if contextTokens != 512 || streamed != "A" || result.Text != "A" || result.Tokens != 1 || result.FinishReason != "eos" || result.DurationMS != 4 {
		t.Fatalf("context = %d, streamed = %q, result = %+v", contextTokens, streamed, result)
	}
}
