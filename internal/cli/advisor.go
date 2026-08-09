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

	"github.com/charmbracelet/glamour"
	waldoai "github.com/openwaldo/waldo/internal/ai"
	"github.com/openwaldo/waldo/internal/config"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/model"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var modelAdvisorInput io.Reader = os.Stdin
var modelAdvisorTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
var modelAdvisorWidth = func() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 60 {
		return 88
	}
	if width > 120 {
		return 120
	}
	return width
}
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
	Build   bool           `json:"build,omitempty"`
}

type advisorIndexEvidence struct {
	Path    string                  `json:"path"`
	Totals  waldoindex.Totals       `json:"totals"`
	Corpora []waldoindex.CorpusInfo `json:"corpora"`
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
	exists, err := model.Exists(root, name)
	if err != nil {
		return err
	}
	creating := !exists
	var report *model.Advice
	if exists {
		value, evidenceErr := currentAdvisorEvidence(root, name)
		if evidenceErr != nil {
			return evidenceErr
		}
		report = &value
	}
	indexEvidence, err := currentAdvisorIndex(configuration)
	if err != nil {
		return err
	}
	var original *model.Compose
	if report != nil {
		original = report.Compose
	}
	allowedCorpora := advisorAllowedCorpora(original, indexEvidence.Corpora)
	draftPath, err := filepath.Abs(name + "-advisor.yaml")
	if err != nil {
		return err
	}
	var draft *model.Compose
	if original != nil {
		copy := *original
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

	if creating {
		fmt.Fprintf(stdout, "Advisor: model %s does not exist. I loaded %d indexed corpora so we can design it from scratch.\n", name, len(indexEvidence.Corpora))
		fmt.Fprintln(stdout, "Advisor: what should this model do, and what hardware and training-time budget should it fit?")
	} else {
		fmt.Fprintf(stdout, "Advisor: loaded model %s (%s) and %d indexed corpora. Ask about its training, configuration, or a next experiment.\n", name, report.State, len(indexEvidence.Corpora))
	}
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
		if !creating {
			value, evidenceErr := currentAdvisorEvidence(root, name)
			if evidenceErr != nil {
				return evidenceErr
			}
			report = &value
		}
		history = append(history, advisorTurn{Role: "user", Content: question})
		prompt := advisorChatPrompt(name, creating, report, draft, indexEvidence, history)
		var answer advisorReply
		for attempt := 1; attempt <= 2; attempt++ {
			fmt.Fprintf(stderr, "Advisor: thinking with %s/%s...\n", selected.Provider, selected.Model)
			response, askErr := modelAdvisorAsk(commandContext.Execution, selected, prompt)
			if askErr != nil {
				return askErr
			}
			answer, err = parseAdvisorReply(response, original, allowedCorpora, creating)
			if err == nil {
				break
			}
			if attempt == 2 {
				return fmt.Errorf("AI advisor did not return a valid response: %w", err)
			}
			prompt += "\n\nYour response was invalid: " + err.Error() + "\nReturn corrected JSON only."
		}
		if err := renderAdvisorReply(stdout, answer.Reply); err != nil {
			return err
		}
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
				if creating {
					build, buildErr := confirmAdvisorBuild(reader, stdout, name, draftPath)
					if buildErr != nil {
						return buildErr
					}
					if build {
						return runModelCompose(commandContext, []string{name, draftPath}, stdout, stderr)
					}
					history = append(history, advisorTurn{Role: "system", Content: "The compose draft was saved, but the operator declined to start training."})
				}
			} else {
				fmt.Fprintln(stdout, "Advisor: compose unchanged.")
				history = append(history, advisorTurn{Role: "system", Content: "The operator declined the proposed compose change."})
			}
		} else if creating && answer.Build {
			if draft == nil {
				return fmt.Errorf("advisor requested a build before creating a compose")
			}
			build, buildErr := confirmAdvisorBuild(reader, stdout, name, draftPath)
			if buildErr != nil {
				return buildErr
			}
			if build {
				return runModelCompose(commandContext, []string{name, draftPath}, stdout, stderr)
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

func currentAdvisorIndex(configuration config.Config) (advisorIndexEvidence, error) {
	root, _, err := config.EffectiveIndexRoot(configuration)
	if err != nil {
		return advisorIndexEvidence{}, err
	}
	target, err := waldoindex.ResolveConfigured(root, "")
	if err != nil {
		return advisorIndexEvidence{}, fmt.Errorf("resolve configured training index: %w", err)
	}
	corpora, err := waldoindex.ListCorpora(target)
	if err != nil {
		return advisorIndexEvidence{}, fmt.Errorf("list configured training corpora: %w", err)
	}
	totals, err := waldoindex.Summarize(target)
	if err != nil {
		return advisorIndexEvidence{}, fmt.Errorf("summarize configured training index: %w", err)
	}
	return advisorIndexEvidence{Path: target.Rel, Totals: totals, Corpora: corpora}, nil
}

func advisorAllowedCorpora(original *model.Compose, corpora []waldoindex.CorpusInfo) map[string]bool {
	allowed := make(map[string]bool, len(corpora))
	for _, corpus := range corpora {
		allowed[corpus.Path] = true
	}
	if original != nil {
		for _, stage := range original.Stages {
			for _, corpus := range stage.Corpora {
				allowed[corpus] = true
			}
		}
	}
	return allowed
}

func advisorChatPrompt(name string, creating bool, report *model.Advice, draft *model.Compose, indexEvidence advisorIndexEvidence, history []advisorTurn) string {
	evidence, _ := json.MarshalIndent(report, "", "  ")
	draftJSON, _ := json.MarshalIndent(draft, "", "  ")
	indexJSON, _ := json.MarshalIndent(indexEvidence, "", "  ")
	referenceJSON, _ := json.MarshalIndent(advisorComposeReference(indexEvidence.Corpora), "", "  ")
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	historyJSON, _ := json.MarshalIndent(history, "", "  ")
	mode := "existing model"
	if creating {
		mode = "new model creation"
	}
	return `You are WALDO's conversational model advisor. Answer the operator's latest message directly and concisely using the supplied model evidence, current telemetry, saved compose, index inventory, and conversation. Distinguish observed facts from recommendations. Format reply as concise Markdown: use short paragraphs and, when presenting several facts, descriptive headings and bullet lists. Do not return one dense paragraph.

In new model creation mode, conduct a natural requirements interview. Establish intended behavior, suitable indexed training corpora, desired model scale, available hardware, wall-clock budget, context needs, and any evaluation or licensing constraints. Ask focused follow-up questions rather than dumping a questionnaire. Once you have enough information, propose a practical complete schema-1 waldo-model-compose. Do not propose it prematurely. In existing model mode, you may explain the model, assess a running job, diagnose a failure, or design a practical next experiment.

When proposing a compose, return it in "compose" and list concise "changes". Otherwise omit both fields. In new model creation mode, "changes" describes the design choices. Set "build" true only when the operator explicitly asks to start a previously saved draft. Never claim a file was changed or training started; WALDO confirms and performs those actions. Never modify a running model. Use only corpus paths listed in the configured training index and consider corpus size, content, and license.

Return exactly one JSON object: {"reply":"Markdown response","changes":["choice"],"compose":{...},"build":false}. Omit changes, compose, and build when unused. Do not use Markdown fences or add other keys.

Requested model name: ` + name + `
Mode: ` + mode + `

Current WALDO evidence:
` + string(evidence) + `

Current editable compose draft (null means unavailable):
` + string(draftJSON) + `

Configured training corpus index:
` + string(indexJSON) + `

Valid compose shape reference (schema example only, not a recommendation):
` + string(referenceJSON) + `

Current executable backends require tokenizer byte@builtin-byte-schema-1 with vocabulary_size 259. Architecture hidden_size must be divisible by attention_heads, attention_heads by key_value_heads, and every sequence_length must not exceed context_tokens. Training stages use type pre-training, fine-tuning, alignment, or other; the currently supported objective is causal-language-modeling. Set positive steps, batch_size, sequence_length, and learning_rate. Use checkpoint_every and evaluate_every appropriate to the run length.

Conversation:
` + string(historyJSON)
}

func advisorComposeReference(corpora []waldoindex.CorpusInfo) map[string]any {
	corpus := "select/an/indexed-corpus"
	if len(corpora) > 0 {
		corpus = corpora[0].Path
	}
	return map[string]any{
		"kind": "waldo-model-compose", "schema": 1,
		"architecture": map[string]any{
			"family": "decoder-transformer", "context_tokens": 512, "vocabulary_size": 259,
			"hidden_size": 384, "intermediate_size": 1024, "layers": 6,
			"attention_heads": 6, "key_value_heads": 2, "tie_embeddings": true,
			"parameter_dtype": "bfloat16",
			"tokenizer":       map[string]any{"name": "byte", "revision": "builtin-byte-schema-1"},
		},
		"stages": []any{map[string]any{
			"name": "pretrain", "type": "pre-training", "objective": "causal-language-modeling",
			"corpora": []string{corpus},
			"parameters": map[string]any{
				"profile": "causal-pretrain-v1", "steps": 1000, "batch_size": 64,
				"sequence_length": 512, "learning_rate": 0.0003, "seed": 42,
				"warmup_steps": 10, "checkpoint_every": 100, "evaluate_every": 100,
			},
		}},
	}
}

func parseAdvisorReply(response string, original *model.Compose, allowedCorpora map[string]bool, creating bool) (advisorReply, error) {
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
	if original == nil && !creating {
		return advisorReply{}, fmt.Errorf("model has no saved compose to revise")
	}
	if len(reply.Changes) == 0 {
		return advisorReply{}, fmt.Errorf("proposed compose requires at least one declared change")
	}
	if err := reply.Compose.Validate(); err != nil {
		return advisorReply{}, fmt.Errorf("validate proposed compose: %w", err)
	}
	for _, stage := range reply.Compose.Stages {
		for _, corpus := range stage.Corpora {
			if !allowedCorpora[corpus] {
				return advisorReply{}, fmt.Errorf("proposed compose introduces corpus %q that is not in the configured index", corpus)
			}
		}
	}
	return reply, nil
}

func confirmAdvisorBuild(reader *bufio.Reader, output io.Writer, name, composePath string) (bool, error) {
	fmt.Fprintf(output, "Advisor: start training model %s from %s now? [y/N] ", name, composePath)
	confirmation, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	if !advisorConfirmed(confirmation) {
		fmt.Fprintln(output, "Advisor: build not started; the compose draft is ready when you are.")
		return false, nil
	}
	fmt.Fprintf(output, "Advisor: starting model %s.\n", name)
	return true, nil
}

func printAdvisorChanges(output io.Writer, changes []string) {
	fmt.Fprintln(output, "Advisor: proposed compose changes:")
	for _, change := range changes {
		fmt.Fprintf(output, "  - %s\n", change)
	}
}

func renderAdvisorReply(output io.Writer, markdown string) error {
	renderer, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(modelAdvisorWidth()))
	if err != nil {
		return fmt.Errorf("initialize advisor Markdown renderer: %w", err)
	}
	rendered, err := renderer.Render("## Advisor\n\n" + strings.TrimSpace(markdown) + "\n")
	if err != nil {
		return fmt.Errorf("render advisor response: %w", err)
	}
	_, err = fmt.Fprint(output, rendered)
	return err
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
