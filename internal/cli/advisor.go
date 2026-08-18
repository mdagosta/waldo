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
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	waldoai "github.com/openwaldo/waldo/internal/ai"
	"github.com/openwaldo/waldo/internal/config"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
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
	Build   bool           `json:"build,omitempty"`
}

type advisorIndexEvidence struct {
	Path    string                  `json:"path"`
	Totals  waldoindex.Totals       `json:"totals"`
	Corpora []waldoindex.CorpusInfo `json:"corpora"`
}

type advisorChatRecord struct {
	Kind        string `json:"kind"`
	Schema      int    `json:"schema"`
	ObservedUTC string `json:"observed_utc"`
	Session     string `json:"session"`
	Role        string `json:"role"`
	Category    string `json:"category"`
	Provider    string `json:"provider,omitempty"`
	AIModel     string `json:"ai_model,omitempty"`
	Content     string `json:"content"`
}

type advisorTranscript struct {
	mutex           sync.Mutex
	root, name      string
	provider, model string
	session         string
	pending         []advisorChatRecord
	history         []advisorTurn
}

type advisorBuildSummary struct {
	Ordinal    int            `json:"ordinal"`
	Stage      string         `json:"stage"`
	State      model.RunState `json:"state"`
	Started    string         `json:"started_utc,omitempty"`
	Finished   string         `json:"finished_utc,omitempty"`
	Attempts   int            `json:"attempts"`
	ResumeStep int64          `json:"resume_step,omitempty"`
	Steps      int64          `json:"steps,omitempty"`
	Tokens     int64          `json:"tokens,omitempty"`
	FinalLoss  *float64       `json:"final_loss,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type synchronizedWriter struct {
	mutex *sync.Mutex
	value io.Writer
}

var advisorDraftNumber = regexp.MustCompile(`^([0-9]{4})-(.+)\.yaml$`)
var legacyAdvisorDraftNumber = regexp.MustCompile(`^(.*)-([0-9]{4})\.yaml$`)

func (writer synchronizedWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.value.Write(data)
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
	transcript, err := newAdvisorTranscript(root, name, selected)
	if err != nil {
		return err
	}
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
	draftPath, draftExists, err := latestAdvisorDraftPath(name)
	if err != nil {
		return err
	}
	var draft *model.Compose
	if original != nil {
		copy := *original
		draft = &copy
	}
	if draftExists {
		loaded, _, loadErr := model.LoadCompose(draftPath)
		if loadErr != nil {
			return fmt.Errorf("load advisor draft: %w", loadErr)
		}
		draft = &loaded
		fmt.Fprintf(stdout, "Advisor: loaded existing draft %s\n", draftPath)
	}

	if creating {
		fmt.Fprintf(stdout, "Advisor: model %s does not exist. I loaded %d indexed corpora so we can design it from scratch.\n", name, len(indexEvidence.Corpora))
		fmt.Fprintln(stdout, "Advisor: what should this model do, and what hardware and training-time budget should it fit?")
	} else {
		fmt.Fprintf(stdout, "Advisor: loaded model %s (%s) and %d indexed corpora. Ask about its training, configuration, or a next experiment.\n", name, report.State, len(indexEvidence.Corpora))
	}
	fmt.Fprintln(stdout, "Advisor: type quit to exit. I will ask before writing any compose change.")
	reader := bufio.NewReader(modelAdvisorInput)
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
		if err := transcript.Record("user", "chat", question); err != nil {
			return err
		}
		var buildHistory []advisorBuildSummary
		var composeHistory []string
		if !creating {
			buildHistory, composeHistory, err = currentAdvisorBuildHistory(root, name)
			if err != nil {
				return err
			}
		}
		prompt := advisorChatPrompt(name, creating, report, draft, indexEvidence, buildHistory, composeHistory, transcript.History())
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
		if err := transcript.Record("assistant", "chat", answer.Reply); err != nil {
			return err
		}
		if answer.Compose != nil {
			printAdvisorChanges(stdout, answer.Changes)
			targetPath, selectErr := selectAdvisorDraftPath(reader, stdout, root, name, draftPath, draft, *answer.Compose)
			if selectErr != nil {
				return selectErr
			}
			fmt.Fprintf(stdout, "Advisor: apply these changes to %s? [y/N] ", targetPath)
			confirmation, confirmErr := reader.ReadString('\n')
			if confirmErr != nil && confirmErr != io.EOF {
				return confirmErr
			}
			if advisorConfirmed(confirmation) {
				if err := writeAdvisorDraft(targetPath, *answer.Compose); err != nil {
					return err
				}
				draftPath = targetPath
				copy := *answer.Compose
				draft = &copy
				fmt.Fprintf(stdout, "Advisor: updated %s\n", draftPath)
				if err := transcript.Record("system", "compose", "The operator approved and WALDO wrote the proposed compose draft."); err != nil {
					return err
				}
				if creating {
					build, buildErr := confirmAdvisorBuild(reader, stdout, name, draftPath)
					if buildErr != nil {
						return buildErr
					}
					if build {
						return runAdvisorBuild(commandContext, name, draftPath, selected, indexEvidence, transcript, stdout, stderr)
					}
					if err := transcript.Record("system", "build", "The compose draft was saved, but the operator declined to start training."); err != nil {
						return err
					}
				}
			} else {
				fmt.Fprintln(stdout, "Advisor: compose unchanged.")
				if err := transcript.Record("system", "compose", "The operator declined the proposed compose change."); err != nil {
					return err
				}
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
				return runAdvisorBuild(commandContext, name, draftPath, selected, indexEvidence, transcript, stdout, stderr)
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

func newAdvisorTranscript(root, name string, selected waldoai.Selection) (*advisorTranscript, error) {
	transcript := &advisorTranscript{
		root: root, name: name, provider: selected.Provider, model: selected.Model,
		session: fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
	}
	file, err := os.Open(filepath.Join(root, name, "advisor", "CHAT.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return transcript, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record advisorChatRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("read advisor chat history: %w", err)
		}
		if record.Kind != "waldo-advisor-chat" || record.Schema != 1 || record.Role == "" || record.Content == "" {
			return nil, fmt.Errorf("advisor chat history contains an invalid record")
		}
		transcript.history = append(transcript.history, advisorTurn{Role: record.Role, Content: record.Content})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(transcript.history) > 24 {
		transcript.history = transcript.history[len(transcript.history)-24:]
	}
	return transcript, nil
}

func (transcript *advisorTranscript) Record(role, category, content string) error {
	transcript.mutex.Lock()
	defer transcript.mutex.Unlock()
	record := advisorChatRecord{
		Kind: "waldo-advisor-chat", Schema: 1, ObservedUTC: time.Now().UTC().Format(time.RFC3339Nano),
		Session: transcript.session, Role: role, Category: category, Provider: transcript.provider,
		AIModel: transcript.model, Content: strings.TrimSpace(content),
	}
	transcript.pending = append(transcript.pending, record)
	transcript.history = append(transcript.history, advisorTurn{Role: role, Content: record.Content})
	if len(transcript.history) > 24 {
		transcript.history = transcript.history[len(transcript.history)-24:]
	}
	return transcript.flushLocked()
}

func (transcript *advisorTranscript) History() []advisorTurn {
	transcript.mutex.Lock()
	defer transcript.mutex.Unlock()
	return append([]advisorTurn(nil), transcript.history...)
}

func (transcript *advisorTranscript) Flush() error {
	transcript.mutex.Lock()
	defer transcript.mutex.Unlock()
	return transcript.flushLocked()
}

func (transcript *advisorTranscript) flushLocked() error {
	if len(transcript.pending) == 0 {
		return nil
	}
	modelPath := filepath.Join(transcript.root, transcript.name)
	if _, err := os.Stat(filepath.Join(modelPath, "MODEL.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	directory := filepath.Join(modelPath, "advisor")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, "CHAT.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	for _, record := range transcript.pending {
		encoded, err := json.Marshal(record)
		if err != nil {
			_ = file.Close()
			return err
		}
		if _, err := file.Write(append(encoded, '\n')); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	transcript.pending = nil
	return nil
}

func currentAdvisorBuildHistory(root, name string) ([]advisorBuildSummary, []string, error) {
	inspection, err := model.Inspect(root, name)
	if err != nil {
		return nil, nil, err
	}
	history := make([]advisorBuildSummary, 0, len(inspection.Runs))
	for index, run := range inspection.Runs {
		pin := inspection.Model.Runs[index]
		summary := advisorBuildSummary{
			Ordinal: pin.Ordinal, Stage: pin.Stage, State: run.State, Started: run.Started,
			Finished: run.Finished, Attempts: len(run.Attempts), Error: run.Error,
		}
		if pin.Resume != nil {
			summary.ResumeStep = pin.Resume.Step
		}
		if run.Progress != nil {
			summary.Steps, summary.Tokens, summary.FinalLoss = run.Progress.Steps, run.Progress.ConsumedTokens, run.Progress.LastLoss
		}
		if run.Observation != nil {
			summary.Steps, summary.Tokens, summary.FinalLoss = run.Observation.Steps, run.Observation.ConsumedTokens, run.Observation.FinalLoss
		}
		history = append(history, summary)
	}
	entries, err := os.ReadDir(filepath.Join(inspection.Path, model.ComposeHistoryDirectory))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	var composes []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			composes = append(composes, entry.Name())
		}
	}
	return history, composes, nil
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
				allowed[corpus.Path] = true
			}
		}
	}
	return allowed
}

func advisorChatPrompt(name string, creating bool, report *model.Advice, draft *model.Compose, indexEvidence advisorIndexEvidence, buildHistory []advisorBuildSummary, composeHistory []string, history []advisorTurn) string {
	evidence, _ := json.MarshalIndent(report, "", "  ")
	draftJSON, _ := json.MarshalIndent(draft, "", "  ")
	indexJSON, _ := json.MarshalIndent(indexEvidence, "", "  ")
	buildJSON, _ := json.MarshalIndent(buildHistory, "", "  ")
	composeHistoryJSON, _ := json.MarshalIndent(composeHistory, "", "  ")
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

When proposing a compose, return it in "compose" and list concise "changes". Otherwise omit both fields. In new model creation mode, "changes" describes the design choices. Set "build" true only when the operator explicitly asks to start a previously saved draft. Never claim a file was changed or training started; WALDO confirms and performs those actions. Archived composes are immutable; architecture or base changes represent a new compose and normally a new model build. Never modify a running model. Use only corpus paths listed in the configured training index and consider corpus size, content, and license.

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

Current executable backends support byte@builtin-byte-schema-1 with vocabulary_size 259, tiktoken/r50k_base@tiktoken-r50k-base with vocabulary_size 50259, and tiktoken/cl100k_base@tiktoken-cl100k-base with vocabulary_size 100259. Architecture hidden_size must be divisible by attention_heads, attention_heads by key_value_heads, and every sequence_length must not exceed context_tokens. Training stages use type pre-training, fine-tuning, alignment, or other; supported objectives are causal-language-modeling and assistant-response-modeling. Set positive steps, batch_size, sequence_length, and learning_rate. Use checkpoint_every and evaluate_every appropriate to the run length.

Durable build history:
` + string(buildJSON) + `

Archived compose history (oldest to newest):
` + string(composeHistoryJSON) + `

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
				"profile": "causal-pretrain-shuffled", "steps": 1000, "batch_size": 64,
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
			if !allowedCorpora[corpus.Path] {
				return advisorReply{}, fmt.Errorf("proposed compose introduces corpus %q that is not in the configured index", corpus.Path)
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

type advisorCheckpointMonitor struct {
	ctx        context.Context
	root       string
	name       string
	selection  waldoai.Selection
	index      advisorIndexEvidence
	transcript *advisorTranscript
	output     io.Writer
	warnings   io.Writer
	events     chan model.Progress
	done       chan struct{}
}

func newAdvisorCheckpointMonitor(ctx context.Context, root, name string, selection waldoai.Selection, index advisorIndexEvidence, transcript *advisorTranscript, output, warnings io.Writer) *advisorCheckpointMonitor {
	monitor := &advisorCheckpointMonitor{
		ctx: ctx, root: root, name: name, selection: selection, index: index,
		transcript: transcript, output: output, warnings: warnings,
		events: make(chan model.Progress, 1), done: make(chan struct{}),
	}
	go monitor.run()
	return monitor
}

func (monitor *advisorCheckpointMonitor) Observe(event model.Progress) {
	_ = monitor.transcript.Flush()
	if event.Training == nil || event.Training.Kind != "checkpoint" {
		return
	}
	select {
	case monitor.events <- event:
	default:
		select {
		case <-monitor.events:
		default:
		}
		select {
		case monitor.events <- event:
		default:
		}
	}
}

func (monitor *advisorCheckpointMonitor) Close() {
	close(monitor.events)
	<-monitor.done
}

func (monitor *advisorCheckpointMonitor) run() {
	defer close(monitor.done)
	for event := range monitor.events {
		report, err := currentAdvisorEvidence(monitor.root, monitor.name)
		if err != nil {
			fmt.Fprintf(monitor.warnings, "warning: advisor checkpoint monitor: %v\n", err)
			continue
		}
		buildHistory, composeHistory, err := currentAdvisorBuildHistory(monitor.root, monitor.name)
		if err != nil {
			fmt.Fprintf(monitor.warnings, "warning: advisor checkpoint monitor: %v\n", err)
			continue
		}
		prompt := advisorMonitorPrompt(report, event, monitor.index, buildHistory, composeHistory, monitor.transcript.History())
		response, err := modelAdvisorAsk(monitor.ctx, monitor.selection, prompt)
		if err != nil {
			if monitor.ctx.Err() == nil {
				fmt.Fprintf(monitor.warnings, "warning: advisor checkpoint monitor: %v\n", err)
			}
			continue
		}
		allowed := advisorAllowedCorpora(report.Compose, monitor.index.Corpora)
		answer, err := parseAdvisorReply(response, report.Compose, allowed, false)
		if err != nil {
			fmt.Fprintf(monitor.warnings, "warning: advisor checkpoint monitor returned invalid advice: %v\n", err)
			continue
		}
		if answer.Compose != nil || answer.Build || len(answer.Changes) != 0 {
			fmt.Fprintln(monitor.warnings, "warning: advisor checkpoint monitor attempted an interactive action; ignoring it")
			continue
		}
		if err := monitor.transcript.Record("assistant", "checkpoint-monitor", answer.Reply); err != nil {
			fmt.Fprintf(monitor.warnings, "warning: persist advisor checkpoint monitor: %v\n", err)
		}
		if err := renderAdvisorMarkdown(monitor.output, fmt.Sprintf("Advisor monitor · step %d", event.Training.Step), answer.Reply); err != nil {
			fmt.Fprintf(monitor.warnings, "warning: render advisor checkpoint monitor: %v\n", err)
		}
	}
}

func advisorMonitorPrompt(report model.Advice, event model.Progress, index advisorIndexEvidence, buildHistory []advisorBuildSummary, composeHistory []string, history []advisorTurn) string {
	payload, _ := json.MarshalIndent(struct {
		Checkpoint     model.Progress        `json:"checkpoint"`
		Model          model.Advice          `json:"model"`
		Index          advisorIndexEvidence  `json:"index"`
		BuildHistory   []advisorBuildSummary `json:"build_history"`
		ComposeHistory []string              `json:"compose_history"`
		ChatHistory    []advisorTurn         `json:"chat_history"`
	}{event, report, index, buildHistory, composeHistory, history}, "", "  ")
	return "You are monitoring a WALDO training build at a durable checkpoint. Give a concise Markdown assessment of progress, loss and held-out-loss trends, throughput, ETA, and any actionable concern. Recommend let-run unless evidence supports inspect or stop. Do not propose or modify a compose in checkpoint monitoring. Return exactly one JSON object with only a reply string: {\"reply\":\"Markdown assessment\"}.\n\nEvidence:\n" + string(payload)
}

func runAdvisorBuild(commandContext Context, name, composePath string, selected waldoai.Selection, index advisorIndexEvidence, transcript *advisorTranscript, stdout, stderr io.Writer) error {
	if err := transcript.Record("system", "build", fmt.Sprintf("Starting model %s from %s.", name, filepath.Base(composePath))); err != nil {
		return err
	}
	var outputMutex sync.Mutex
	lockedOutput := synchronizedWriter{mutex: &outputMutex, value: stdout}
	lockedProgress := synchronizedWriter{mutex: &outputMutex, value: stderr}
	root, err := configuredModelRoot()
	if err != nil {
		return err
	}
	monitor := newAdvisorCheckpointMonitor(commandContext.Execution, root, name, selected, index, transcript, lockedOutput, lockedProgress)
	buildContext := commandContext
	buildContext.Progress = monitor.Observe
	buildErr := runModelComposeTraining(buildContext, name, composePath, training.Cluster{}, lockedOutput, lockedProgress)
	monitor.Close()
	result := "Build completed."
	if buildErr != nil {
		result = "Build ended: " + buildErr.Error()
	}
	if err := transcript.Record("system", "build", result); err != nil && buildErr == nil {
		return err
	}
	_ = transcript.Flush()
	return buildErr
}

func printAdvisorChanges(output io.Writer, changes []string) {
	fmt.Fprintln(output, "Advisor: proposed compose changes:")
	for _, change := range changes {
		fmt.Fprintf(output, "  - %s\n", change)
	}
}

func renderAdvisorReply(output io.Writer, markdown string) error {
	return renderAdvisorMarkdown(output, "Advisor", markdown)
}

func renderAdvisorMarkdown(output io.Writer, title, markdown string) error {
	return renderTerminalMarkdown(output, "## "+title+"\n\n"+strings.TrimSpace(markdown))
}

func advisorConfirmed(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "y" || value == "yes"
}

func latestAdvisorDraftPath(name string) (string, bool, error) {
	stem := name + "-advisor"
	base, err := filepath.Abs("0000-" + stem + ".yaml")
	if err != nil {
		return "", false, err
	}
	directory := filepath.Dir(base)
	legacyBase := stem + ".yaml"
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", false, err
	}
	best, bestOrdinal := "", -1
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		candidate := entry.Name()
		ordinal := -1
		if candidate == legacyBase {
			ordinal = 0
		} else if match := advisorDraftNumber.FindStringSubmatch(candidate); match != nil && match[2] == stem {
			ordinal, _ = strconv.Atoi(match[1])
		} else if match := legacyAdvisorDraftNumber.FindStringSubmatch(candidate); match != nil && match[1] == stem {
			ordinal, _ = strconv.Atoi(match[2])
		}
		if ordinal > bestOrdinal {
			best, bestOrdinal = candidate, ordinal
		}
	}
	if best == "" {
		return base, false, nil
	}
	return filepath.Join(directory, best), true, nil
}

func selectAdvisorDraftPath(reader *bufio.Reader, output io.Writer, root, name, currentPath string, current *model.Compose, proposed model.Compose) (string, error) {
	if _, err := os.Stat(currentPath); errors.Is(err, os.ErrNotExist) {
		return currentPath, nil
	} else if err != nil {
		return "", err
	}
	structural := current == nil || !reflect.DeepEqual(current.Architecture, proposed.Architecture) || !reflect.DeepEqual(current.Base, proposed.Base)
	archived, err := advisorComposeArchived(filepath.Join(root, name), current)
	if err != nil {
		return "", err
	}
	defaultNew := structural || archived
	defaultLabel := "y/N"
	if defaultNew {
		defaultLabel = "Y/n"
	}
	fmt.Fprintf(output, "Advisor: save this as a new compose instead of updating %s? [%s] ", currentPath, defaultLabel)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	useNew := defaultNew
	if answer == "y" || answer == "yes" {
		useNew = true
	} else if answer == "n" || answer == "no" {
		useNew = false
	}
	if !useNew {
		return currentPath, nil
	}
	path, err := nextAdvisorDraftPath(currentPath)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(output, "Advisor: new compose will be %s\n", path)
	return path, nil
}

func nextAdvisorDraftPath(currentPath string) (string, error) {
	directory, filename := filepath.Dir(currentPath), filepath.Base(currentPath)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	start := 0
	if match := advisorDraftNumber.FindStringSubmatch(filename); match != nil {
		stem = match[2]
		ordinal, _ := strconv.Atoi(match[1])
		start = ordinal + 1
	} else if match := legacyAdvisorDraftNumber.FindStringSubmatch(filename); match != nil {
		stem = match[1]
		ordinal, _ := strconv.Atoi(match[2])
		start = ordinal + 1
	}
	for ordinal := start; ordinal <= 9999; ordinal++ {
		path := filepath.Join(directory, fmt.Sprintf("%04d-%s.yaml", ordinal, stem))
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("advisor compose drafts exceed 9999 revisions")
}

func advisorComposeArchived(modelPath string, compose *model.Compose) (bool, error) {
	if compose == nil {
		return false, nil
	}
	entries, err := os.ReadDir(filepath.Join(modelPath, model.ComposeHistoryDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		archived, _, err := model.LoadCompose(filepath.Join(modelPath, model.ComposeHistoryDirectory, entry.Name()))
		if err != nil {
			return false, err
		}
		if reflect.DeepEqual(archived, *compose) {
			return true, nil
		}
	}
	return false, nil
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
