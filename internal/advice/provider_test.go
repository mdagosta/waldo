// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package advice

import "testing"

func TestSelectPrefersOpenAIAndHonorsOverride(t *testing.T) {
	environment := map[string]string{"OPENAI_API_KEY": "openai-secret", "ANTHROPIC_API_KEY": "anthropic-secret"}
	getenv := func(name string) string { return environment[name] }
	automatic, err := Select("auto", "", Credentials{}, getenv)
	if err != nil || automatic.Provider != ProviderOpenAI || automatic.Model != DefaultOpenAIModel || automatic.Key != "openai-secret" {
		t.Fatalf("automatic selection = %+v, err = %v", automatic, err)
	}
	override, err := Select("anthropic", "custom", Credentials{}, getenv)
	if err != nil || override.Provider != ProviderAnthropic || override.Model != "custom" || override.Key != "anthropic-secret" {
		t.Fatalf("override selection = %+v, err = %v", override, err)
	}
}

func TestSelectFallsBackAndFailsClosed(t *testing.T) {
	getenv := func(string) string { return "" }
	selected, err := Select("", "", Credentials{}, getenv)
	if err != nil || selected.Provider != ProviderNone || selected.Key != "" {
		t.Fatalf("fallback selection = %+v, err = %v", selected, err)
	}
	if _, err := Select("openai", "", Credentials{}, getenv); err == nil {
		t.Fatal("missing explicit OpenAI key accepted")
	}
	if _, err := Select("local", "", Credentials{}, getenv); err == nil {
		t.Fatal("unimplemented local provider accepted")
	}
}

func TestSelectUsesConfiguredKeysAndEnvironmentOverrides(t *testing.T) {
	configured := Credentials{OpenAI: "stored-openai", Anthropic: "stored-anthropic"}
	selected, err := Select("anthropic", "", configured, func(string) string { return "" })
	if err != nil || selected.Key != "stored-anthropic" {
		t.Fatalf("configured selection = %+v, err = %v", selected, err)
	}
	selected, err = Select("openai", "", configured, func(name string) string {
		if name == "OPENAI_API_KEY" {
			return "environment-openai"
		}
		return ""
	})
	if err != nil || selected.Key != "environment-openai" {
		t.Fatalf("environment selection = %+v, err = %v", selected, err)
	}
}
