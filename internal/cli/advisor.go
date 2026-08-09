// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	waldoai "github.com/openwaldo/waldo/internal/ai"
	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/model"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var modelAdvisorInput io.Reader = os.Stdin
var modelAdvisorTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
var modelAdvisorAsk = func(ctx context.Context, selection waldoai.Selection, prompt string) (string, error) {
	return (waldoai.Client{}).Ask(ctx, selection, prompt)
}

type advisorTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type advisorReply struct {
	Reply   string         `json:"reply"`
	Changes []string       `json:"changes,omitempty"`
	Compose *model.Compose `json:"compose,omitempty"`
}

func runModelAdvisor(commandContext Context, args []string, stdout, stderr io.Writer) error {
	if commandContext.JSON {
		return fmt.Errorf("model advisor is an interactive chat and does not support --json")
	}
	if !modelAdvisorTerminal() {
		return fmt.Errorf("model advisor requires an interactive terminal")
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	root, err := config.EffectiveModelRoot(configuration)
	if err != nil {
		return err
	}
	provider := stringOption(commandContext, "provider")
	if provider == "" {
		provider = configuration.AI.Provider
	}
	aiModel := stringOption(commandContext, "model")
	if aiModel == "" {
		aiModel = configuration.AI.Model
	}
	selected, err := waldoai.Select(provider, aiModel, waldoai.Credentials{APIKey: configuration.AI.APIKey}, nil)
	if err != nil {
		return err
	}
	if selected.Provider == waldoai.ProviderNone {
		return fmt.Errorf("model advisor requires ai.provider openai or anthropic and a corresponding API key")
	}

	name := args[0]
	report, err := currentAdvisorEvidence(root, name)
	if err != nil {
		return err
	}
	draftPath, err := filepath.Abs(name + "-advisor.yaml")
	if err != nil {
		return err
	}
	var draft *model.Compose
	if report.Compose != nil {
		copy := *report.Compose
		draft = &copy
	}
	if _, statErr := os.Stat(draftPath); statErr == nil {
		loaded, _, loadErr := model.LoadCompose(draftPath)
		if loadErr != nil {
			return fmt.Errorf("load advisor draft: %w", loadErr)
		}
		draft = &loaded
		fmt.Fprintf(stdout, "Advisor: loaded existing draft %s\n", draftPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	fmt.Fprintf(stdout, "Advisor: loaded model %s (%s). Ask about its training, configuration, or a next experiment.\n", name, report.State)
	fmt.Fprintln(stdout, "Advisor: type quit to exit. I will ask before writing any compose change.")
	reader := bufio.NewReader(modelAdvisorInput)
	var history []advisorTurn
	for {
		fmt.Fprint(stdout, "You: ")
		question, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		question = strings.TrimSpace(question)
		if question == "" {
			if readErr == io.EOF {
				return nil
			}
			continue
		}
		if question == "quit" || question == "exit" || question == "/quit" {
			return nil
		}
		report, err = currentAdvisorEvidence(root, name)
		if err != nil {
			return err
		}
		history = append(history, advisorTurn{Role: "user", Content: question})
		prompt := advisorChatPrompt(report, draft, history)
		var answer advisorReply
		for attempt := 1; attempt <= 2; attempt++ {
			fmt.Fprintf(stderr, "Advisor: thinking with %s/%s...\n", selected.Provider, selected.Model)
			response, askErr := modelAdvisorAsk(commandContext.Execution, selected, prompt)
			if askErr != nil {
				return askErr
			}
			answer, err = parseAdvisorReply(response, report.Compose)
			if err == nil {
				break
			}
			if attempt == 2 {
				return fmt.Errorf("AI advisor did not return a valid response: %w", err)
			}
			prompt += "\n\nYour response was invalid: " + err.Error() + "\nReturn corrected JSON only."
		}
		fmt.Fprintf(stdout, "Advisor: %s\n", answer.Reply)
		history = append(history, advisorTurn{Role: "assistant", Content: answer.Reply})
		if answer.Compose != nil {
			printAdvisorChanges(stdout, answer.Changes)
			fmt.Fprintf(stdout, "Advisor: apply these changes to %s? [y/N] ", draftPath)
			confirmation, confirmErr := reader.ReadString('\n')
			if confirmErr != nil && confirmErr != io.EOF {
				return confirmErr
			}
			if advisorConfirmed(confirmation) {
				if err := writeAdvisorDraft(draftPath, *answer.Compose); err != nil {
					return err
				}
				copy := *answer.Compose
				draft = &copy
				fmt.Fprintf(stdout, "Advisor: updated %s\n", draftPath)
				history = append(history, advisorTurn{Role: "system", Content: "The operator approved and WALDO wrote the proposed compose draft."})
			} else {
				fmt.Fprintln(stdout, "Advisor: compose unchanged.")
				history = append(history, advisorTurn{Role: "system", Content: "The operator declined the proposed compose change."})
			}
		}
		if readErr == io.EOF {
			return nil
		}
	}
}

func currentAdvisorEvidence(root, name string) (model.Advice, error) {
	inspection, err := model.Inspect(root, name)
	if err != nil {
		return model.Advice{}, err
	}
	return model.BuildAdvice(inspection, time.Now())
}

func advisorChatPrompt(report model.Advice, draft *model.Compose, history []advisorTurn) string {
	evidence, _ := json.MarshalIndent(report, "", "  ")
	draftJSON, _ := json.MarshalIndent(draft, "", "  ")
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	historyJSON, _ := json.MarshalIndent(history, "", "  ")
	return `You are WALDO's conversational model advisor. Answer the operator's latest message directly and concisely using the supplied model evidence, current telemetry, saved compose, and conversation. Distinguish observed facts from recommendations. You may explain the model, assess a running job, diagnose a failure, or design a practical next experiment.

If the operator asks you to modify or create the next compose, return a complete proposed schema-1 waldo-model-compose in "compose" and list concise "changes". Otherwise omit both fields. Never claim a file was changed; WALDO asks the operator for confirmation and performs the write. Never modify the running model. Do not invent corpus paths or introduce paths absent from the saved compose. If there is no saved compose, explain that compose editing is unavailable.

Return exactly one JSON object: {"reply":"plain conversational response","changes":["change"],"compose":{...}}. Omit changes and compose when no edit is proposed. Do not use Markdown fences or add other keys.

Current WALDO evidence:
` + string(evidence) + `

Current editable compose draft (null means unavailable):
` + string(draftJSON) + `

Conversation:
` + string(historyJSON)
}

func parseAdvisorReply(response string, original *model.Compose) (advisorReply, error) {
	start, end := strings.IndexByte(response, '{'), strings.LastIndexByte(response, '}')
	if start < 0 || end < start {
		return advisorReply{}, fmt.Errorf("response does not contain a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(response[start : end+1])))
	decoder.DisallowUnknownFields()
	var reply advisorReply
	if err := decoder.Decode(&reply); err != nil {
		return advisorReply{}, fmt.Errorf("decode response: %w", err)
	}
	if strings.TrimSpace(reply.Reply) == "" {
		return advisorReply{}, fmt.Errorf("response requires reply text")
	}
	if reply.Compose == nil {
		if len(reply.Changes) != 0 {
			return advisorReply{}, fmt.Errorf("changes require a proposed compose")
		}
		return reply, nil
	}
	if original == nil {
		return advisorReply{}, fmt.Errorf("model has no saved compose to revise")
	}
	if len(reply.Changes) == 0 {
		return advisorReply{}, fmt.Errorf("proposed compose requires at least one declared change")
	}
	if err := reply.Compose.Validate(); err != nil {
		return advisorReply{}, fmt.Errorf("validate proposed compose: %w", err)
	}
	allowed := map[string]bool{}
	for _, stage := range original.Stages {
		for _, corpus := range stage.Corpora {
			allowed[corpus] = true
		}
	}
	for _, stage := range reply.Compose.Stages {
		for _, corpus := range stage.Corpora {
			if !allowed[corpus] {
				return advisorReply{}, fmt.Errorf("proposed compose introduces undeclared corpus %q", corpus)
			}
		}
	}
	return reply, nil
}

func printAdvisorChanges(output io.Writer, changes []string) {
	fmt.Fprintln(output, "Advisor: proposed compose changes:")
	for _, change := range changes {
		fmt.Fprintf(output, "  - %s\n", change)
	}
}

func advisorConfirmed(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "y" || value == "yes"
}

func writeAdvisorDraft(path string, compose model.Compose) error {
	data, err := yaml.Marshal(compose)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".waldo-advisor-*.yaml")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}
