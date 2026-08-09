// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

func TestAdvisorProposalValidatesComposeAndCorpusBoundary(t *testing.T) {
	original := advisorTestCompose()
	proposal := advisorProposal{Assessment: "test one change", Changes: []string{"increase steps"}, Compose: original}
	encoded, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseAdvisorProposal(string(encoded), original)
	if err != nil || parsed.Assessment != proposal.Assessment {
		t.Fatalf("proposal = %+v, err = %v", parsed, err)
	}
	proposal.Compose = advisorTestCompose()
	proposal.Compose.Stages[0].Corpora = []string{"invented/corpus"}
	encoded, err = json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAdvisorProposal(string(encoded), original); err == nil || !strings.Contains(err.Error(), "introduces undeclared corpus") {
		t.Fatalf("corpus boundary error = %v", err)
	}
}

func TestAdvisorComposeNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.yaml")
	if _, err := writeAdvisorCompose(path, advisorTestCompose()); err != nil {
		t.Fatal(err)
	}
	if _, err := writeAdvisorCompose(path, advisorTestCompose()); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite error = %v", err)
	}
}

func TestAdvisorQuestionUsesAnswerAndDefault(t *testing.T) {
	var output strings.Builder
	answer, err := advisorQuestion(bufio.NewReader(strings.NewReader("better completion\n")), &output, "Goal? ", "")
	if err != nil || answer != "better completion" || output.String() != "Goal? " {
		t.Fatalf("answer = %q, output = %q, err = %v", answer, output.String(), err)
	}
	answer, err = advisorQuestion(bufio.NewReader(strings.NewReader("\n")), &output, "Budget? ", "same budget")
	if err != nil || answer != "same budget" {
		t.Fatalf("default answer = %q, err = %v", answer, err)
	}
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
