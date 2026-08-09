// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

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
	configured := Credentials{APIKey: "stored-key"}
	selected, err := Select("anthropic", "", configured, func(string) string { return "" })
	if err != nil || selected.Key != "stored-key" {
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

func TestSelectDoesNotInferProviderFromConfiguredKey(t *testing.T) {
	_, err := Select("auto", "", Credentials{APIKey: "stored-key"}, func(string) string { return "" })
	if err == nil || err.Error() != "ai.provider must be openai or anthropic when ai.api-key is set" {
		t.Fatalf("error = %v", err)
	}
}

func TestClientAsksOpenAI(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "test-model" || body["input"] != "evidence" || body["store"] != false {
			t.Errorf("body = %+v", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"output":[{"content":[{"type":"output_text","text":"let it run"}]}]}`))}, nil
	})}
	response, err := (Client{HTTP: httpClient, OpenAIURL: "https://openai.example/v1/responses"}).Ask(context.Background(), Selection{Provider: ProviderOpenAI, Model: "test-model", Key: "secret"}, "evidence")
	if err != nil || response != "let it run" {
		t.Fatalf("response = %q, err = %v", response, err)
	}
}

func TestClientAsksAnthropic(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-Api-Key") != "secret" || request.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Errorf("headers = %+v", request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"content":[{"type":"text","text":"inspect the loss"}]}`))}, nil
	})}
	response, err := (Client{HTTP: httpClient, AnthropicURL: "https://anthropic.example/v1/messages"}).Ask(context.Background(), Selection{Provider: ProviderAnthropic, Model: "test-model", Key: "secret"}, "evidence")
	if err != nil || response != "inspect the loss" {
		t.Fatalf("response = %q, err = %v", response, err)
	}
}
