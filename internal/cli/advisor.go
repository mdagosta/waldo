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

type advisorAnswers struct {
	Goal     string `json:"goal"`
	Budget   string `json:"budget"`
	Priority string `json:"priority"`
}

type advisorProposal struct {
	Assessment string        `json:"assessment"`
	Changes    []string      `json:"changes"`
	Compose    model.Compose `json:"compose"`
}

type advisorOutput struct {
	SourceModel string          `json:"source_model"`
	Provider    string          `json:"provider"`
	AIModel     string          `json:"ai_model"`
	Output      string          `json:"output"`
	Answers     advisorAnswers  `json:"answers"`
	Proposal    advisorProposal `json:"proposal"`
}

func runModelAdvisor(commandContext Context, args []string, stdout, stderr io.Writer) error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	root, err := config.EffectiveModelRoot(configuration)
	if err != nil {
		return err
	}
	inspection, err := model.Inspect(root, args[0])
	if err != nil {
		return err
	}
	report, err := model.BuildAdvice(inspection, time.Now())
	if err != nil {
		return err
	}
	if report.Compose == nil {
		return fmt.Errorf("model %q has no saved compose to revise", args[0])
	}
	answers, err := collectAdvisorAnswers(commandContext, stderr)
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

	prompt := advisorPrompt(report, answers)
	var proposal advisorProposal
	for attempt := 1; attempt <= 2; attempt++ {
		if !commandContext.JSON {
			fmt.Fprintf(stderr, "ai/advisor             contacting %s/%s (attempt %d/2)\n", selected.Provider, selected.Model, attempt)
		}
		response, askErr := modelAdvisorAsk(commandContext.Execution, selected, prompt)
		if askErr != nil {
			return askErr
		}
		proposal, err = parseAdvisorProposal(response, *report.Compose)
		if err == nil {
			break
		}
		if attempt == 2 {
			return fmt.Errorf("AI advisor did not produce a valid compose after two attempts: %w", err)
		}
		prompt += "\n\nYour previous response was invalid: " + err.Error() + "\nReturn a corrected JSON object only. Previous response:\n" + truncateAdvisorResponse(response)
	}
	outputPath := stringOption(commandContext, "output")
	if outputPath == "" {
		outputPath = args[0] + "-advisor.yaml"
	}
	absolute, err := writeAdvisorCompose(outputPath, proposal.Compose)
	if err != nil {
		return err
	}
	result := advisorOutput{SourceModel: args[0], Provider: selected.Provider, AIModel: selected.Model, Output: absolute, Answers: answers, Proposal: proposal}
	if commandContext.JSON {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "ASSESSMENT: %s\n", proposal.Assessment)
	if len(proposal.Changes) > 0 {
		fmt.Fprintln(stdout, "CHANGES:")
		for _, change := range proposal.Changes {
			fmt.Fprintf(stdout, "  - %s\n", change)
		}
	}
	fmt.Fprintf(stdout, "COMPOSE:    %s\n", absolute)
	fmt.Fprintf(stdout, "TEST WITH:  waldo model compose %s-advisor %s\n", args[0], absolute)
	return nil
}

func collectAdvisorAnswers(commandContext Context, output io.Writer) (advisorAnswers, error) {
	answers := advisorAnswers{
		Goal: strings.TrimSpace(stringOption(commandContext, "goal")), Budget: strings.TrimSpace(stringOption(commandContext, "budget")),
		Priority: strings.TrimSpace(stringOption(commandContext, "priority")),
	}
	interactive := !commandContext.JSON && modelAdvisorTerminal()
	if !interactive && answers.Goal == "" {
		return advisorAnswers{}, fmt.Errorf("model advisor requires --goal outside an interactive terminal")
	}
	reader := bufio.NewReader(modelAdvisorInput)
	var err error
	if interactive && answers.Goal == "" {
		answers.Goal, err = advisorQuestion(reader, output, "What should the next model be able to do? ", "")
		if err != nil {
			return advisorAnswers{}, err
		}
	}
	if interactive && answers.Budget == "" {
		answers.Budget, err = advisorQuestion(reader, output, "What hardware and training-time budget should it fit? ", "same hardware and duration as the current compose")
		if err != nil {
			return advisorAnswers{}, err
		}
	}
	if interactive && answers.Priority == "" {
		answers.Priority, err = advisorQuestion(reader, output, "What should improve first? ", "held-out quality and useful behavior")
		if err != nil {
			return advisorAnswers{}, err
		}
	}
	if answers.Budget == "" {
		answers.Budget = "same hardware and duration as the current compose"
	}
	if answers.Priority == "" {
		answers.Priority = "held-out quality and useful behavior"
	}
	return answers, nil
}

func advisorQuestion(reader *bufio.Reader, output io.Writer, question, defaultValue string) (string, error) {
	fmt.Fprint(output, question)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	if value == "" {
		return "", fmt.Errorf("an answer is required")
	}
	return value, nil
}

func advisorPrompt(report model.Advice, answers advisorAnswers) string {
	evidence, _ := json.MarshalIndent(report, "", "  ")
	constraints, _ := json.MarshalIndent(answers, "", "  ")
	return "You are the WALDO model advisor. Design one practical follow-up experiment from the saved compose and observed training evidence. Respect the operator's goal and budget. Use only corpus paths already present in the source compose. Do not modify the running model. Return exactly one JSON object with keys assessment (string), changes (array of concise strings), and compose (a complete schema-1 waldo-model-compose object). Do not use Markdown fences or add other keys. The compose must be internally consistent and immediately testable by `waldo model compose`. Prefer the smallest experiment that tests your hypothesis.\n\nOperator answers:\n" + string(constraints) + "\n\nWALDO evidence:\n" + string(evidence)
}

func parseAdvisorProposal(response string, original model.Compose) (advisorProposal, error) {
	start, end := strings.IndexByte(response, '{'), strings.LastIndexByte(response, '}')
	if start < 0 || end < start {
		return advisorProposal{}, fmt.Errorf("response does not contain a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(response[start : end+1])))
	decoder.DisallowUnknownFields()
	var proposal advisorProposal
	if err := decoder.Decode(&proposal); err != nil {
		return advisorProposal{}, fmt.Errorf("decode proposal: %w", err)
	}
	if strings.TrimSpace(proposal.Assessment) == "" || len(proposal.Changes) == 0 {
		return advisorProposal{}, fmt.Errorf("proposal requires an assessment and at least one declared change")
	}
	if err := proposal.Compose.Validate(); err != nil {
		return advisorProposal{}, fmt.Errorf("validate proposed compose: %w", err)
	}
	allowed := map[string]bool{}
	for _, stage := range original.Stages {
		for _, corpus := range stage.Corpora {
			allowed[corpus] = true
		}
	}
	for _, stage := range proposal.Compose.Stages {
		for _, corpus := range stage.Corpora {
			if !allowed[corpus] {
				return advisorProposal{}, fmt.Errorf("proposed compose introduces undeclared corpus %q", corpus)
			}
		}
	}
	return proposal, nil
}

func writeAdvisorCompose(path string, compose model.Compose) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	data, err := yaml.Marshal(compose)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("advisor output %s already exists; choose another --output path", absolute)
		}
		return "", err
	}
	written := false
	defer func() {
		_ = file.Close()
		if !written {
			_ = os.Remove(absolute)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	written = true
	return absolute, nil
}

func truncateAdvisorResponse(response string) string {
	const limit = 16 * 1024
	if len(response) <= limit {
		return response
	}
	return response[:limit]
}
