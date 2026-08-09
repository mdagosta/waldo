// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

func TestAdvisorReplySupportsConversationAndValidatesComposeBoundary(t *testing.T) {
	original := advisorTestCompose()
	allowed := advisorAllowedCorpora(&original, []waldoindex.CorpusInfo{{Path: "core/code"}})
	plain, err := parseAdvisorReply(`{"reply":"The held-out loss is improving."}`, &original, allowed, false)
	if err != nil || plain.Reply == "" || plain.Compose != nil {
		t.Fatalf("plain reply = %+v, err = %v", plain, err)
	}
	proposed := advisorReply{Reply: "Try a slightly longer run.", Changes: []string{"increase steps"}, Compose: &original}
	encoded, err := json.Marshal(proposed)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseAdvisorReply(string(encoded), &original, allowed, false)
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
	if _, err := parseAdvisorReply(string(encoded), &original, allowed, false); err == nil || !strings.Contains(err.Error(), "not in the configured index") {
		t.Fatalf("corpus boundary error = %v", err)
	}
	indexed := advisorTestCompose()
	indexed.Stages[0].Corpora = []string{"core/code"}
	proposed.Compose = &indexed
	encoded, err = json.Marshal(proposed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdvisorReply(string(encoded), &original, allowed, false); err != nil {
		t.Fatalf("indexed corpus proposal was rejected: %v", err)
	}
	if _, err := parseAdvisorReply(string(encoded), nil, allowed, true); err != nil {
		t.Fatalf("new-model compose was rejected: %v", err)
	}
	if _, err := parseAdvisorReply(string(encoded), nil, allowed, false); err == nil || !strings.Contains(err.Error(), "no saved compose") {
		t.Fatalf("uncomposed existing model error = %v", err)
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

func TestAdvisorReplyRendersMarkdown(t *testing.T) {
	previousWidth := modelAdvisorWidth
	modelAdvisorWidth = func() int { return 72 }
	t.Cleanup(func() { modelAdvisorWidth = previousWidth })
	var output bytes.Buffer
	if err := renderAdvisorReply(&output, "### Architecture\n\n- 6 layers\n- 9.5M parameters"); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	if !strings.Contains(rendered, "Advisor") || !strings.Contains(rendered, "Architecture") || !strings.Contains(rendered, "6 layers") || !strings.Contains(rendered, "9.5M parameters") {
		t.Fatalf("rendered advisor response = %q", rendered)
	}
}

func TestAdvisorComposeReferenceIsValid(t *testing.T) {
	encoded, err := json.Marshal(advisorComposeReference([]waldoindex.CorpusInfo{{Path: "core/books"}}))
	if err != nil {
		t.Fatal(err)
	}
	var compose model.Compose
	if err := json.Unmarshal(encoded, &compose); err != nil {
		t.Fatal(err)
	}
	if err := compose.Validate(); err != nil {
		t.Fatalf("advisor compose reference is invalid: %v", err)
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
