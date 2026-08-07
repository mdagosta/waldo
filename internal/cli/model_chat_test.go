// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/inference"
)

func TestParseModelChatSupportsOneShotGenerationOptions(t *testing.T) {
	name, prompt, options, err := parseModelChat([]string{"foo", "hello world", "--max-tokens", "12", "--temperature", "0", "--top-p", "1", "--seed", "9"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "foo" || prompt == nil || *prompt != "hello world" || options.MaxTokens != 12 || options.Temperature != 0 || options.TopP != 1 || options.Seed == nil || *options.Seed != 9 {
		t.Fatalf("name = %q, prompt = %v, options = %+v", name, prompt, options)
	}
	if _, _, _, err := parseModelChat([]string{"foo", "--temperature", "-1"}); err == nil {
		t.Fatal("negative temperature accepted")
	}
	if _, _, _, err := parseModelChat([]string{"foo", "--top-p", "NaN"}); err == nil {
		t.Fatal("NaN top-p accepted")
	}
}

func TestOneShotChatStreamsSafeTerminalOutputAndReturnsJSON(t *testing.T) {
	opened := inference.Opened{Description: inference.Description{Model: "foo", RunID: "run"}, Session: &chatSession{data: []byte{'A', 0x1b, 0xff}}}
	options := inference.Options{MaxTokens: 3, Temperature: 0, TopP: 1}
	var output bytes.Buffer
	if err := runOneShotChat(Context{Execution: context.Background()}, opened, "hello", options, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "A\\x1b\\xff\n" {
		t.Fatalf("safe output = %q", output.String())
	}
	output.Reset()
	if err := runOneShotChat(Context{Execution: context.Background(), JSON: true}, opened, "hello", options, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"model": "foo"`, `"prompt": "hello"`, `"finish_reason": "max_tokens"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("JSON = %s", output.String())
		}
	}
}

func TestInteractiveChatMaintainsAndClearsContext(t *testing.T) {
	session := &chatSession{data: []byte(" answer")}
	opened := inference.Opened{Description: inference.Description{Model: "foo", Backend: "mlx", ContextTokens: 512}, Session: session}
	previous := modelChatInput
	modelChatInput = strings.NewReader("first\n/clear\nsecond\n/exit\n")
	defer func() { modelChatInput = previous }()
	var output bytes.Buffer
	if err := runInteractiveChat(context.Background(), opened, inference.Options{MaxTokens: 2, Temperature: 0, TopP: 1}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "raw causal continuation") || len(session.prompts) != 2 || session.prompts[0] != "first" || session.prompts[1] != "second" {
		t.Fatalf("output = %q, prompts = %#v", output.String(), session.prompts)
	}
}

func TestBoundChatHistoryUsesByteTokenizerContext(t *testing.T) {
	if got := boundChatHistory("123456789", 4); got != "6789" {
		t.Fatalf("bounded history = %q", got)
	}
}

type chatSession struct {
	data    []byte
	prompts []string
}

func (session *chatSession) Generate(_ context.Context, prompt string, _ inference.Options, emit func(inference.Token) error) (inference.Result, error) {
	session.prompts = append(session.prompts, prompt)
	for _, value := range session.data {
		if emit != nil {
			if err := emit(inference.Token{Bytes: []byte{value}}); err != nil {
				return inference.Result{}, err
			}
		}
	}
	return inference.Result{Text: strings.ToValidUTF8(string(session.data), "�"), Tokens: len(session.data), FinishReason: "max_tokens", DurationMS: 1}, nil
}

func (*chatSession) Close() error { return nil }
