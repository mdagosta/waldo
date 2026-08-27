// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package modelexport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

func TestJinjaInteractionTemplatesPreserveDeclaredCapability(t *testing.T) {
	for _, prompt := range []string{model.InteractionUserAssistantV1, model.InteractionChatMLV1} {
		plain, err := jinjaInteractionTemplate(model.Interaction{Template: prompt})
		if err != nil {
			t.Fatal(err)
		}
		if plain == "" || strings.Contains(plain, "tools") || strings.Contains(plain, "tool_calls") {
			t.Fatalf("plain %s template advertises tools:\n%s", prompt, plain)
		}
		tools, err := jinjaInteractionTemplate(model.Interaction{Template: prompt, Tools: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"tools | tojson", "tool_calls", "message['role']", "message['content']", "add_generation_prompt"} {
			if !strings.Contains(tools, required) {
				t.Errorf("tool %s template does not preserve %q:\n%s", prompt, required, tools)
			}
		}
	}
}

func TestRawInteractionHasNoChatTemplate(t *testing.T) {
	template, err := jinjaInteractionTemplate(model.Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	if template != "" {
		t.Fatalf("raw template = %q", template)
	}
}

func TestHuggingFaceAndMLXTokenizerConfigurationCarriesInteraction(t *testing.T) {
	record := model.ModelRecord{Architecture: model.Architecture{ContextTokens: 2048}, Interaction: model.Interaction{Template: model.InteractionUserAssistantV1, Tools: true}}
	configuration, template, err := huggingFaceTokenizerConfiguration(record)
	if err != nil {
		t.Fatal(err)
	}
	if template == "" || configuration["chat_template"] != template || !strings.Contains(template, "tool_calls") {
		t.Fatalf("tokenizer interaction = %q / %+v", template, configuration)
	}
}

func TestGGUFMetadataCarriesInteraction(t *testing.T) {
	record := model.ModelRecord{
		Name: "fixture",
		Architecture: model.Architecture{
			Family: "decoder-transformer", ContextTokens: 128, VocabularySize: 259,
			HiddenSize: 64, IntermediateSize: 192, Layers: 2, AttentionHeads: 4, KeyValueHeads: 2,
			TieEmbeddings: true, ParameterDType: "float32", Tokenizer: model.Tokenizer{Name: "byte", Revision: "builtin-byte-schema-1"},
		},
		Interaction: model.Interaction{Template: model.InteractionChatMLV1, Tools: true},
	}
	metadata, err := modelGGUFMetadata(record)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range metadata {
		if entry.key == "tokenizer.chat_template" {
			found = strings.Contains(entry.value.(string), "tool_calls")
		}
	}
	if !found {
		t.Fatalf("GGUF metadata omitted the declared tool interaction: %+v", metadata)
	}
}

func TestReleaseBOMCarriesInteraction(t *testing.T) {
	interaction := model.Interaction{Template: model.InteractionUserAssistantV1, Tools: true}
	data, err := json.Marshal(releaseBOM{Interaction: interaction})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Interaction model.Interaction `json:"interaction"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Interaction != interaction {
		t.Fatalf("release interaction = %+v, want %+v", decoded.Interaction, interaction)
	}
}

func TestExportInteractionPreservesLegacyCompletedToolRun(t *testing.T) {
	inspection := model.Inspection{
		Model: model.ModelRecord{Interaction: model.Interaction{Template: model.InteractionUserAssistantV1}},
		Runs:  []model.RunRecord{{State: model.RunComplete}},
		RunBOMs: []model.RunBOM{{
			Objective: "assistant-response-modeling",
			Conversation: training.ConversationTransform{
				Template: model.InteractionUserAssistantV1, SupervisedRoles: []string{"assistant"}, Tools: true,
			},
		}},
	}
	if interaction := inspection.EffectiveInteraction(); !interaction.Tools {
		t.Fatalf("legacy completed run interaction = %+v", interaction)
	}
	inspection.Runs[0].State = model.RunInterrupted
	if interaction := inspection.EffectiveInteraction(); interaction.Tools {
		t.Fatalf("incomplete legacy run unexpectedly enabled tools: %+v", interaction)
	}
}
