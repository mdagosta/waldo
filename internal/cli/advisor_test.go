// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

func TestAdvisorReplySupportsConversationAndValidatesComposeBoundary(t *testing.T) {
	original := advisorTestCompose()
	plain, err := parseAdvisorReply(`{"reply":"The held-out loss is improving."}`, &original)
	if err != nil || plain.Reply == "" || plain.Compose != nil {
		t.Fatalf("plain reply = %+v, err = %v", plain, err)
	}
	proposed := advisorReply{Reply: "Try a slightly longer run.", Changes: []string{"increase steps"}, Compose: &original}
	encoded, err := json.Marshal(proposed)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseAdvisorReply(string(encoded), &original)
	if err != nil || parsed.Compose == nil {
		t.Fatalf("proposal = %+v, err = %v", parsed, err)
	}
	bad := advisorTestCompose()
	bad.Stages[0].Corpora = []string{"invented/corpus"}
	proposed.Compose = &bad
	encoded, err = json.Marshal(proposed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdvisorReply(string(encoded), &original); err == nil || !strings.Contains(err.Error(), "introduces undeclared corpus") {
		t.Fatalf("corpus boundary error = %v", err)
	}
}

func TestAdvisorDraftUpdatesAtomicallyAfterConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.yaml")
	first := advisorTestCompose()
	if err := writeAdvisorDraft(path, first); err != nil {
		t.Fatal(err)
	}
	second := advisorTestCompose()
	second.Stages[0].Parameters.Steps = 3
	if err := writeAdvisorDraft(path, second); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := model.LoadCompose(path)
	if err != nil || loaded.Stages[0].Parameters.Steps != 3 {
		t.Fatalf("draft = %+v, err = %v", loaded, err)
	}
	if mode := fileMode(t, path); mode != 0o644 {
		t.Fatalf("draft mode = %o", mode)
	}
	if !advisorConfirmed("yes\n") || !advisorConfirmed("Y") || advisorConfirmed("no") || advisorConfirmed("") {
		t.Fatal("confirmation parsing is incorrect")
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func advisorTestCompose() model.Compose {
	return model.Compose{
		Kind: "waldo-model-compose", Schema: 1,
		Architecture: model.Architecture{
			Family: "decoder-transformer", ContextTokens: 128, VocabularySize: 259,
			HiddenSize: 64, IntermediateSize: 192, Layers: 2, AttentionHeads: 4,
			KeyValueHeads: 2, TieEmbeddings: true, ParameterDType: "float32",
			Tokenizer: model.Tokenizer{Name: "byte", Revision: "builtin-byte-schema-1"},
		},
		Stages: []model.Stage{{
			Name: "pretrain", Type: "pre-training", Objective: "causal-language-modeling",
			Corpora:    []string{"core/books/gutenberg"},
			Parameters: training.Parameters{Steps: 2, BatchSize: 1, SequenceLength: 64, LearningRate: 0.001, Seed: 7},
		}},
	}
}
