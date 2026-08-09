// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package advice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	ProviderAuto          = "auto"
	ProviderOpenAI        = "openai"
	ProviderAnthropic     = "anthropic"
	ProviderLocal         = "local"
	ProviderNone          = "deterministic"
	DefaultOpenAIModel    = "gpt-5.6-terra"
	DefaultAnthropicModel = "claude-sonnet-4-20250514"
)

type Selection struct {
	Provider string
	Model    string
	Key      string
}

type Credentials struct{ OpenAI, Anthropic string }

func Select(requested, model string, credentials Credentials, getenv func(string) string) (Selection, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	provider := strings.ToLower(strings.TrimSpace(requested))
	if provider == "" {
		provider = ProviderAuto
	}
	openAI, anthropic := getenv("OPENAI_API_KEY"), getenv("ANTHROPIC_API_KEY")
	if openAI == "" {
		openAI = credentials.OpenAI
	}
	if anthropic == "" {
		anthropic = credentials.Anthropic
	}
	if provider == ProviderAuto {
		switch {
		case openAI != "":
			provider = ProviderOpenAI
		case anthropic != "":
			provider = ProviderAnthropic
		default:
			provider = ProviderNone
		}
	}
	switch provider {
	case ProviderOpenAI:
		if openAI == "" {
			return Selection{}, fmt.Errorf("OpenAI advice requires OPENAI_API_KEY")
		}
		if model == "" {
			model = DefaultOpenAIModel
		}
		return Selection{Provider: provider, Model: model, Key: openAI}, nil
	case ProviderAnthropic:
		if anthropic == "" {
			return Selection{}, fmt.Errorf("Anthropic advice requires ANTHROPIC_API_KEY")
		}
		if model == "" {
			model = DefaultAnthropicModel
		}
		return Selection{Provider: provider, Model: model, Key: anthropic}, nil
	case ProviderNone:
		return Selection{Provider: provider}, nil
	case ProviderLocal:
		return Selection{}, fmt.Errorf("local advice is reserved but not yet implemented")
	default:
		return Selection{}, fmt.Errorf("advice provider must be auto, openai, anthropic, deterministic, or local")
	}
}

type Client struct {
	HTTP                    *http.Client
	OpenAIURL, AnthropicURL string
}

func (client Client) Ask(ctx context.Context, selected Selection, prompt string) (string, error) {
	switch selected.Provider {
	case ProviderOpenAI:
		return client.askOpenAI(ctx, selected, prompt)
	case ProviderAnthropic:
		return client.askAnthropic(ctx, selected, prompt)
	default:
		return "", fmt.Errorf("provider %q does not support API advice", selected.Provider)
	}
}

func (client Client) request(ctx context.Context, url string, body any, headers map[string]string, output any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	httpClient := client.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("advisor API returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

func (client Client) askOpenAI(ctx context.Context, selected Selection, prompt string) (string, error) {
	url := client.OpenAIURL
	if url == "" {
		url = "https://api.openai.com/v1/responses"
	}
	var response struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	err := client.request(ctx, url, map[string]any{"model": selected.Model, "input": prompt, "store": false}, map[string]string{"authorization": "Bearer " + selected.Key}, &response)
	if err != nil {
		return "", err
	}
	var text []string
	for _, item := range response.Output {
		for _, content := range item.Content {
			if content.Type == "output_text" && content.Text != "" {
				text = append(text, content.Text)
			}
		}
	}
	if len(text) == 0 {
		return "", fmt.Errorf("OpenAI advisor returned no text")
	}
	return strings.Join(text, "\n"), nil
}

func (client Client) askAnthropic(ctx context.Context, selected Selection, prompt string) (string, error) {
	url := client.AnthropicURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}
	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	err := client.request(ctx, url, map[string]any{"model": selected.Model, "max_tokens": 2048, "messages": []map[string]string{{"role": "user", "content": prompt}}}, map[string]string{"x-api-key": selected.Key, "anthropic-version": "2023-06-01"}, &response)
	if err != nil {
		return "", err
	}
	var text []string
	for _, content := range response.Content {
		if content.Type == "text" && content.Text != "" {
			text = append(text, content.Text)
		}
	}
	if len(text) == 0 {
		return "", fmt.Errorf("Anthropic advisor returned no text")
	}
	return strings.Join(text, "\n"), nil
}
