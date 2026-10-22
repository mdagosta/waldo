// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/openwaldo/waldo/internal/model"
	"gopkg.in/yaml.v3"
)

// Files a merged Hugging Face package takes from its first input; inputs must agree on every one.
var mergeSharedFiles = []string{"config.json", "generation_config.json", "tokenizer_config.json", "special_tokens_map.json", "tokenization_openwaldo.py", "chat_template.jinja", "architecture.py"}

var templateVariable = regexp.MustCompile(`\$(?:\$|([A-Za-z_][A-Za-z0-9_]*)|\{([A-Za-z_][A-Za-z0-9_]*)\})`)

// fillTemplate substitutes $name and ${name}, with $$ for a literal $, in a run's compose templates.
func fillTemplate(text string, values map[string]string) (string, error) {
	var missing []string
	filled := templateVariable.ReplaceAllStringFunc(text, func(match string) string {
		if match == "$$" {
			return "$"
		}
		name := strings.Trim(match, "${}")
		value, ok := values[name]
		if !ok {
			missing = append(missing, name)
		}
		return value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("compose template uses unknown variables: %s", strings.Join(missing, ", "))
	}
	return filled, nil
}

// pieceSeed is the same for every replica of one piece of work and different across pieces.
func pieceSeed(parts ...any) int {
	words := make([]string, len(parts))
	for index, part := range parts {
		words[index] = fmt.Sprint(part)
	}
	digest := sha256.Sum256([]byte(strings.Join(words, "-")))
	return int(digest[0])<<16 | int(digest[1])<<8 | int(digest[2])
}

func (joiner *Joiner) modelName(number int, suffix string) string {
	return fmt.Sprintf("waldo-%s-r%04d-%s", joiner.plan.Name, number, suffix)
}

// values are a stage's compose template inputs for one unit.
func (joiner *Joiner) values(stage, unit string) (map[string]string, error) {
	pretrain := joiner.plan.Pretrain
	switch stage {
	case "bootstrap":
		var sources []string
		for _, name := range joiner.plan.Units {
			sources = append(sources, pretrain.Shards[name]...)
		}
		encoded, _ := json.Marshal(sources)
		return map[string]string{"budget": fmt.Sprintf("tokens: %d", pretrain.BootstrapTokens), "sources": string(encoded)}, nil
	case "pretrain":
		template, err := os.ReadFile(joiner.planFile(pretrain.Compose))
		if err != nil {
			return nil, err
		}
		batch, sequence := templateInt(template, "batch_size"), templateInt(template, "sequence_length")
		if batch == 0 || sequence == 0 {
			return nil, fmt.Errorf("%s needs batch_size and sequence_length", pretrain.Compose)
		}
		steps := (pretrain.TokensPerRound + int64(batch*sequence) - 1) / int64(batch*sequence)
		encoded, _ := json.Marshal(pretrain.Shards[unit])
		return map[string]string{"budget": fmt.Sprintf("steps: %d\n      epochs: 1000", steps), "sources": string(encoded)}, nil
	}
	posttrain := joiner.plan.Posttrain
	if posttrain.Corpus == "" {
		return nil, fmt.Errorf("post-training needs posttrain.corpus, an index path that every unit takes a slice of")
	}
	template, err := os.ReadFile(joiner.planFile(posttrain.Compose))
	if err != nil {
		return nil, err
	}
	line := regexp.MustCompile(`(?m)^( *)- path: \$corpus *$`).FindSubmatch(template)
	if line == nil {
		return nil, fmt.Errorf("%s must list `- path: $corpus` on its own line", posttrain.Compose)
	}
	index := -1
	for position, name := range joiner.plan.Units {
		if name == unit {
			index = position
		}
	}
	indent := string(line[1]) + "  "
	corpus := fmt.Sprintf("%s\n%sfilter:\n%s  slice: {index: %d, count: %d}", posttrain.Corpus, indent, indent, index, len(joiner.plan.Units))
	return map[string]string{"budget": fmt.Sprintf("epochs: %d", posttrain.EpochsPerRound), "corpus": corpus}, nil
}

func templateInt(template []byte, key string) int {
	match := regexp.MustCompile(key + `: (\d+)`).FindSubmatch(template)
	if match == nil {
		return 0
	}
	value, _ := strconv.Atoi(string(match[1]))
	return value
}

func (joiner *Joiner) planFile(value string) string {
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, value[2:])
		}
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(joiner.runDir, value)
}

// compose fills a stage template for one unit.
func (joiner *Joiner) compose(stage, base string, seed int, budget string, values map[string]string) (string, error) {
	compose := joiner.plan.Pretrain.Compose
	if stage == "posttrain" {
		compose = joiner.plan.Posttrain.Compose
	}
	template, err := os.ReadFile(joiner.planFile(compose))
	if err != nil {
		return "", err
	}
	fill := map[string]string{"base_block": "", "seed": strconv.Itoa(seed), "budget": budget, "sources": "[]", "corpus": "",
		"learning_rate": "0.0005", "weight_decay": "0.05", "extra": ""}
	if base != "" {
		fill["base_block"] = "base:\n  model: " + base
	}
	for key, value := range values {
		if key != "budget" {
			fill[key] = value
		}
	}
	return fillTemplate(string(template), fill)
}

func (joiner *Joiner) waldo(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, joiner.Waldo, args...)
	command.Env = append(os.Environ(), "WALDO_CONFIG="+joiner.config())
	command.Stdout, command.Stderr = joiner.Log, joiner.Log
	fmt.Fprintf(joiner.Log, "$ waldo %s\n", strings.Join(args, " "))
	if err := command.Run(); err != nil {
		return fmt.Errorf("waldo %s: %w", args[0]+" "+args[1], err)
	}
	return nil
}

// waldoOutput runs waldo like waldo() but returns its standard output.
func (joiner *Joiner) waldoOutput(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, joiner.Waldo, args...)
	command.Env = append(os.Environ(), "WALDO_CONFIG="+joiner.config())
	command.Stderr = joiner.Log
	fmt.Fprintf(joiner.Log, "$ waldo %s\n", strings.Join(args, " "))
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("waldo %s: %w", strings.Join(args[1:3], " "), err)
	}
	return output, nil
}

func (joiner *Joiner) config() string {
	return filepath.Join(joiner.Home, "waldo-config.json")
}

func (joiner *Joiner) models() string {
	return filepath.Join(joiner.Home, "models")
}

func (joiner *Joiner) complete(name string) bool {
	inspection, err := model.Inspect(joiner.models(), name)
	return err == nil && (inspection.BOM.CurrentRunID != "" || inspection.BOM.CurrentOriginSHA256 != "")
}

// train trains name from the compose text unless an earlier pass already did.
func (joiner *Joiner) train(ctx context.Context, name, text string) error {
	if joiner.complete(name) {
		fmt.Fprintf(joiner.Log, "   %s already trained\n", name)
		return nil
	}
	path := filepath.Join(joiner.workDir(), "composes", name+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return err
	}
	return joiner.waldo(ctx, "model", "train", name, path)
}

// telemetry is the last run's TELEMETRY.csv rows by column.
func (joiner *Joiner) telemetry(name string) ([]map[string]string, error) {
	runs, err := filepath.Glob(filepath.Join(joiner.models(), name, "runs", "*", "TELEMETRY.csv"))
	if err != nil || len(runs) == 0 {
		return nil, fmt.Errorf("model %s has no telemetry", name)
	}
	sort.Strings(runs)
	file, err := os.Open(runs[len(runs)-1])
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("read %s telemetry: %v", name, err)
	}
	var result []map[string]string
	for _, row := range rows[1:] {
		values := map[string]string{}
		for index, column := range rows[0] {
			if index < len(row) {
				values[column] = row[index]
			}
		}
		result = append(result, values)
	}
	return result, nil
}

func (joiner *Joiner) maxTelemetry(name, column string) (float64, error) {
	rows, err := joiner.telemetry(name)
	if err != nil {
		return 0, err
	}
	best := 0.0
	for _, row := range rows {
		if value, err := strconv.ParseFloat(row[column], 64); err == nil && value > best {
			best = value
		}
	}
	return best, nil
}

func (joiner *Joiner) export(ctx context.Context, name string) (string, error) {
	target := filepath.Join(joiner.workDir(), "exports", name)
	if _, err := os.Stat(filepath.Join(target, "model.safetensors")); err == nil {
		return target, nil
	}
	_ = os.RemoveAll(target)
	return target, joiner.waldo(ctx, "model", "export", name, target, "--format", "huggingface", "--allow-incomplete")
}

// fileHashes pins every file a record publishes, so none can be swapped afterwards.
func fileHashes(directory string) (map[string]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		digest, err := hashFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		result[entry.Name()] = digest
	}
	return result, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (joiner *Joiner) store() (string, error) {
	return filepath.Join(strings.TrimPrefix(joiner.plan.Store, "file://"), joiner.plan.Name), nil
}

// publish copies an export into the run's store and returns its URL.
func (joiner *Joiner) publish(ctx context.Context, directory string) (string, error) {
	if joiner.plan.Store == "huggingface" {
		user := joiner.plan.Identities[joiner.Identity].HuggingFace
		if user == "" {
			return "", fmt.Errorf("%s has no hf user in the plan's identities; Hugging Face stores need one", joiner.Identity)
		}
		repository := user + "/" + filepath.Base(directory)
		fmt.Fprintf(joiner.Log, "   publishing %s to huggingface://%s\n", filepath.Base(directory), repository)
		revision, err := uploadHuggingFace(ctx, repository, directory, "waldo "+joiner.plan.Name+": "+filepath.Base(directory), joiner.Log)
		if err != nil {
			return "", err
		}
		return "huggingface://" + repository + "@" + revision, nil
	}
	store, err := joiner.store()
	if err != nil {
		return "", err
	}
	target := filepath.Join(store, filepath.Base(directory))
	if _, err := os.Stat(target); os.IsNotExist(err) {
		fmt.Fprintf(joiner.Log, "   publishing %s to %s\n", filepath.Base(directory), target)
		if err := copyDirectory(directory, target); err != nil {
			return "", err
		}
	}
	return "file://" + target, nil
}

// fetch copies a record's files from the store and checks them against the hashes it commits to.
func (joiner *Joiner) fetch(ctx context.Context, name, identity, sha string, files map[string]string, url string) (string, error) {
	if files == nil {
		files = map[string]string{"model.safetensors": sha}
	}
	if files["model.safetensors"] != sha {
		return "", fmt.Errorf("%s: its files and model_sha256 disagree", name)
	}
	repository, revision := "", ""
	if strings.HasPrefix(url, "huggingface://") {
		at := strings.LastIndex(url, "@")
		if at < 0 {
			return "", fmt.Errorf("%s: %s pins no revision", name, url)
		}
		repository, revision = url[len("huggingface://"):at], url[at+1:]
		if owner, _, _ := strings.Cut(repository, "/"); owner != joiner.plan.Identities[identity].HuggingFace {
			return "", fmt.Errorf("%s: %s is not in %s's Hugging Face account", name, url, identity)
		}
	} else if !strings.HasPrefix(url, "file://") {
		return "", fmt.Errorf("%s: %s is not in a supported store", name, url)
	}
	target := filepath.Join(joiner.workDir(), "exports", name)
	if _, err := os.Stat(filepath.Join(target, "model.safetensors")); os.IsNotExist(err) {
		partial := target + ".partial"
		_ = os.RemoveAll(partial)
		if err := os.MkdirAll(partial, 0o755); err != nil {
			return "", err
		}
		var names []string
		for file := range files {
			names = append(names, file)
		}
		sort.Strings(names)
		if repository != "" {
			fmt.Fprintf(joiner.Log, "   fetching %s\n", url)
			if err := downloadHuggingFace(ctx, repository, revision, partial, names); err != nil {
				return "", err
			}
		} else {
			for _, file := range names {
				if err := copyFile(filepath.Join(strings.TrimPrefix(url, "file://"), file), filepath.Join(partial, file)); err != nil {
					return "", err
				}
			}
		}
		_ = os.RemoveAll(target)
		if err := os.Rename(partial, target); err != nil {
			return "", err
		}
	}
	actual, err := fileHashes(target)
	if err != nil {
		return "", err
	}
	for file, digest := range files {
		if actual[file] != digest {
			_ = os.RemoveAll(target)
			return "", fmt.Errorf("%s: %s in the store does not match the record's hash", name, file)
		}
	}
	return target, nil
}

// ensure imports a fetched package as a local model so composes can name it as a base.
func (joiner *Joiner) ensure(name, directory string) error {
	if joiner.complete(name) {
		return nil
	}
	_, err := (model.Puller{Root: joiner.models()}).PullDirectory(name, directory)
	return err
}

func (joiner *Joiner) base(ctx context.Context, state State, number int) (*MergeRecord, error) {
	if number == 0 {
		return nil, nil
	}
	record := state.Rounds[number-1].Merge
	directory, err := joiner.fetch(ctx, record.Model, record.Identity, record.ModelSHA256, record.Files, record.URL)
	if err != nil {
		return nil, err
	}
	return record, joiner.ensure(record.Model, directory)
}

func (joiner *Joiner) contribute(ctx context.Context, state State, action Action) error {
	name := joiner.modelName(action.Round, action.Unit+"-"+joiner.Identity)
	fmt.Fprintf(joiner.Log, "\n== Round %d (%s), unit %s: %s trains %s ==\n", action.Round, action.Stage, action.Unit, joiner.Identity, name)
	base, err := joiner.base(ctx, state, action.Round)
	if err != nil {
		return err
	}
	values, err := joiner.values(action.Stage, action.Unit)
	if err != nil {
		return err
	}
	stage := action.Stage
	if stage == "bootstrap" {
		stage = "pretrain"
	}
	baseName := ""
	if base != nil {
		baseName = base.Model
	}
	text, err := joiner.compose(stage, baseName, pieceSeed(joiner.plan.Name, action.Round, action.Unit), values["budget"], values)
	if err != nil {
		return err
	}
	if err := joiner.train(ctx, name, text); err != nil {
		return err
	}
	if action.Stage != "bootstrap" && joiner.plan.MinStepsPerRound > 0 {
		steps, err := joiner.maxTelemetry(name, "planned_steps")
		if err != nil {
			return err
		}
		if int(steps) < joiner.plan.MinStepsPerRound {
			return fmt.Errorf("%s trained only %d steps; a round needs at least %d (min_steps_per_round)", name, int(steps), joiner.plan.MinStepsPerRound)
		}
	}
	export, err := joiner.export(ctx, name)
	if err != nil {
		return err
	}
	tokens, err := joiner.maxTelemetry(name, "tokens")
	if err != nil {
		return err
	}
	files, err := fileHashes(export)
	if err != nil {
		return err
	}
	url, err := joiner.publish(ctx, export)
	if err != nil {
		return err
	}
	record := Submission{
		Kind: SubmissionKind, Schema: 1, Run: joiner.plan.Name, Round: action.Round, Stage: action.Stage, Unit: action.Unit,
		Identity: joiner.Identity, Model: name, ModelSHA256: files["model.safetensors"], Files: files, Tokens: int64(tokens),
		URL: url, CreatedAt: joiner.timestamp(),
	}
	if base != nil {
		record.Base, record.BaseSHA256 = &base.Model, &base.ModelSHA256
	}
	if digest, ok := files["BOM.json"]; ok {
		record.BOMSHA256 = &digest
	}
	path := filepath.Join(roundDir(joiner.runDir, action.Round), "submissions", action.Unit+"-"+joiner.Identity+".yaml")
	won, err := joiner.commit(ctx, path, record, fmt.Sprintf("%s round %d: %s by %s", joiner.plan.Name, action.Round, action.Unit, joiner.Identity))
	if err == nil && !won {
		err = fmt.Errorf("round %d unit %s: the runs repo did not accept this submission; on GitHub, the verifier's comment on its pull request says why", action.Round, action.Unit)
	}
	return err
}

func (joiner *Joiner) timestamp() string {
	return joiner.Now().UTC().Format("2006-01-02T15:04:05+00:00")
}

// probe is the held-out loss the verifier itself measures on an export's
// exact weights, never the contributor's own report. seed must be the unit's
// training seed, so the probe scores the records every replica held out.
func (joiner *Joiner) probe(ctx context.Context, stage, export string, seed int, values map[string]string) (model.Evaluation, error) {
	name := filepath.Base(export)
	if err := joiner.ensure(name, export); err != nil {
		return model.Evaluation{}, err
	}
	probeValues := map[string]string{}
	for _, key := range []string{"sources", "corpus"} {
		if value, ok := values[key]; ok {
			probeValues[key] = value
		}
	}
	text, err := joiner.compose(stage, "", seed, "tokens: 16384", probeValues)
	if err != nil {
		return model.Evaluation{}, err
	}
	path := filepath.Join(joiner.workDir(), "composes", fmt.Sprintf("%s-probe-%d.yaml", name, seed))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return model.Evaluation{}, err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return model.Evaluation{}, err
	}
	output, err := joiner.waldoOutput(ctx, "--json", "model", "train", name, path, "--evaluate-only")
	if err != nil {
		return model.Evaluation{}, err
	}
	var evaluation model.Evaluation
	if err := json.Unmarshal(output, &evaluation); err != nil {
		return model.Evaluation{}, fmt.Errorf("read evaluation of %s: %w", name, err)
	}
	return evaluation, nil
}

// verify returns the index of the replica most others agree with, among
// those agreeing with at least half of the others, or -1 without a majority. Honest replicas improve alike from the round's start; a
// free-rider does not, so gaps are judged against each round's progress.
func (joiner *Joiner) verify(ctx context.Context, stage, label string, exports []string, records []Submission, values map[string]string, base *MergeRecord, round int, unit string) (int, *UnitEvidence, error) {
	seed := pieceSeed(joiner.plan.Name, round, unit)
	if len(exports) == 1 {
		return 0, nil, nil
	}
	evidence := &UnitEvidence{Unit: unit}
	var start float64
	if base != nil {
		measured, err := joiner.probe(ctx, stage, filepath.Join(joiner.workDir(), "exports", base.Model), seed, values)
		if err != nil {
			return -1, nil, err
		}
		start = measured.Loss
	} else {
		// A bootstrap starts from random weights, whose loss is about uniform over the vocabulary.
		template, err := os.ReadFile(joiner.planFile(joiner.plan.Pretrain.Compose))
		if err != nil {
			return -1, nil, err
		}
		vocabulary := templateInt(template, "vocabulary_size")
		if vocabulary < 2 {
			return -1, nil, fmt.Errorf("%s needs vocabulary_size to judge bootstrap replicas", joiner.plan.Pretrain.Compose)
		}
		start = math.Log(float64(vocabulary))
	}
	evidence.StartLoss = start
	losses := make([]float64, len(exports))
	for index, export := range exports {
		measured, err := joiner.probe(ctx, stage, export, seed, values)
		if err != nil {
			return -1, nil, err
		}
		losses[index] = measured.Loss
		evidence.EvaluationSetSHA256 = measured.Set.SHA256
		evidence.Replicas = append(evidence.Replicas, ReplicaEvidence{Identity: records[index].Identity, ModelSHA256: records[index].ModelSHA256, HeldoutLoss: measured.Loss})
	}
	fmt.Fprintf(joiner.Log, "   round start probe loss %.4f on this unit's data\n", start)
	agree := make([]int, len(exports))
	for first := range exports {
		for second := first + 1; second < len(exports); second++ {
			progress := math.Max(math.Max(start-losses[first], start-losses[second]), 1e-9)
			gap := math.Abs(losses[first]-losses[second]) / progress
			close := gap <= joiner.plan.Tolerance() || math.Abs(losses[first]-losses[second]) <= joiner.plan.Floor()
			verdict := "DISAGREE"
			if close {
				verdict = "agree"
				agree[first]++
				agree[second]++
			}
			fmt.Fprintf(joiner.Log, "   replicas %d vs %d: probe loss %.4f vs %.4f (gap %.0f%% of the round's progress)  %s\n",
				first+1, second+1, losses[first], losses[second], gap*100, verdict)
		}
	}
	var accepted, rejected []string
	winner := -1
	for index, count := range agree {
		if count >= len(exports)/2 {
			// The most widely agreed replica wins, so one that some peers dispute never beats one none dispute.
			if winner < 0 || count > agree[winner] {
				winner = index
			}
			accepted = append(accepted, strconv.Itoa(index+1))
		} else {
			rejected = append(rejected, strconv.Itoa(index+1))
		}
	}
	if len(accepted) <= len(exports)/2 {
		fmt.Fprintf(joiner.Log, "%s: no majority of replicas agree within %.0f%% of the round's progress; not merging\n", label, joiner.plan.Tolerance()*100)
		return -1, evidence, nil
	}
	message := fmt.Sprintf("   %s: accepted replica %d", label, winner+1)
	if len(rejected) > 0 {
		message += ", rejected replica " + strings.Join(rejected, ", ")
	}
	fmt.Fprintln(joiner.Log, message)
	return winner, evidence, nil
}

func (joiner *Joiner) close(ctx context.Context, state State, action Action) error {
	fmt.Fprintf(joiner.Log, "\n== Round %d (%s): %s closes it ==\n", action.Round, action.Stage, joiner.Identity)
	base, err := joiner.base(ctx, state, action.Round)
	if err != nil {
		return err
	}
	stage := action.Stage
	if stage == "bootstrap" {
		stage = "pretrain"
	}
	submitted := state.Rounds[action.Round].Submissions
	var accepted []Submission
	var evidence []UnitEvidence
	dropped := []DroppedUnit{}
	for _, unit := range joiner.plan.RoundUnits(action.Stage) {
		records := submitted[unit]
		var identities []string
		for _, record := range records {
			identities = append(identities, record.Identity)
		}
		if len(records) < joiner.plan.Replicas {
			dropped = append(dropped, DroppedUnit{Unit: unit, Reason: fmt.Sprintf("%d of %d replicas by the deadline", len(records), joiner.plan.Replicas)})
			continue
		}
		wrongBase := false
		for _, record := range records {
			if (base == nil) != (record.BaseSHA256 == nil) || (base != nil && *record.BaseSHA256 != base.ModelSHA256) {
				wrongBase = true
			}
		}
		if wrongBase {
			dropped = append(dropped, DroppedUnit{Unit: unit, Reason: "a replica did not start from the round's merged weights", Identities: identities})
			continue
		}
		tokens := map[int64]bool{}
		for _, record := range records {
			tokens[record.Tokens] = true
		}
		if len(tokens) > 1 {
			dropped = append(dropped, DroppedUnit{Unit: unit, Reason: "replicas report different tokens trained", Identities: identities})
			continue
		}
		var exports []string
		var present []Submission
		for _, record := range records {
			directory, err := joiner.fetch(ctx, record.Model, record.Identity, record.ModelSHA256, record.Files, record.URL)
			if err != nil {
				fmt.Fprintf(joiner.Log, "   %v\n", err)
				continue
			}
			exports = append(exports, directory)
			present = append(present, record)
		}
		winner := -1
		if len(present) >= joiner.plan.Replicas {
			values, err := joiner.values(action.Stage, unit)
			if err != nil {
				return err
			}
			var unitEvidence *UnitEvidence
			winner, unitEvidence, err = joiner.verify(ctx, stage, "unit "+unit, exports, present, values, base, action.Round, unit)
			if unitEvidence != nil {
				evidence = append(evidence, *unitEvidence)
			}
			if err != nil {
				return err
			}
		}
		if winner < 0 {
			dropped = append(dropped, DroppedUnit{Unit: unit, Reason: "replicas disagree", Identities: identities})
			continue
		}
		accepted = append(accepted, present[winner])
	}
	if len(accepted) == 0 {
		return fmt.Errorf("round %d: no unit was accepted; the run cannot continue without new submissions", action.Round)
	}
	// Named by the units it merges, so a retried close with a different accepted set never reuses another set's merge.
	digest := sha256.New()
	for _, submission := range accepted {
		fmt.Fprintf(digest, "%s %s\n", submission.Unit, submission.ModelSHA256)
	}
	name := joiner.modelName(action.Round, "merge-"+hex.EncodeToString(digest.Sum(nil))[:12])
	target := filepath.Join(joiner.workDir(), "exports", name)
	method := "single"
	if _, err := os.Stat(filepath.Join(target, "model.safetensors")); os.IsNotExist(err) {
		_ = os.RemoveAll(target)
		if len(accepted) == 1 {
			// One accepted unit (always so for the bootstrap) is the merge.
			if err := copyDirectory(filepath.Join(joiner.workDir(), "exports", accepted[0].Model), target); err != nil {
				return err
			}
		} else if err := joiner.merge(ctx, name, accepted, target); err != nil {
			return err
		}
	}
	if len(accepted) > 1 {
		method = "token-weighted-mean"
	}
	if err := joiner.ensure(name, target); err != nil {
		return err
	}
	fmt.Fprintf(joiner.Log, "-- Merge round %d: %d units accepted, %d dropped\n", action.Round, len(accepted), len(dropped))
	files, err := fileHashes(target)
	if err != nil {
		return err
	}
	url, err := joiner.publish(ctx, target)
	if err != nil {
		return err
	}
	record := MergeRecord{
		Kind: MergeKind, Schema: 1, Run: joiner.plan.Name, Round: action.Round, Stage: action.Stage, Identity: joiner.Identity,
		Method: method, Dropped: dropped, Evidence: evidence, Model: name, ModelSHA256: files["model.safetensors"], Files: files, URL: url, ClosedAt: joiner.timestamp(),
	}
	if base != nil {
		record.Base, record.BaseSHA256 = &base.Model, &base.ModelSHA256
	}
	for _, submission := range accepted {
		record.Accepted = append(record.Accepted, AcceptedUnit{Unit: submission.Unit, Identity: submission.Identity, Model: submission.Model, ModelSHA256: submission.ModelSHA256, Tokens: submission.Tokens})
	}
	path := filepath.Join(roundDir(joiner.runDir, action.Round), "merge.yaml")
	won, err := joiner.commit(ctx, path, record, fmt.Sprintf("%s round %d: merge by %s", joiner.plan.Name, action.Round, joiner.Identity))
	if err != nil || won {
		return err
	}
	var theirs MergeRecord
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &theirs) != nil {
		return fmt.Errorf("round %d was merged by someone else, and their record could not be read", action.Round)
	}
	verdict := "DIFFERS from"
	if theirs.ModelSHA256 == record.ModelSHA256 {
		verdict = "matches"
	}
	fmt.Fprintf(joiner.Log, "   %s merged round %d first; their hash %s this one\n", theirs.Identity, action.Round, verdict)
	return nil
}

// merge imports the accepted exports and composes their token-weighted
// merge in waldo, whose records pin every parent and their shared ancestry;
// the exported merge carries that lineage to the next round.
func (joiner *Joiner) merge(ctx context.Context, name string, accepted []Submission, target string) error {
	interaction, err := joiner.interaction()
	if err != nil {
		return err
	}
	text := "kind: waldo-model-compose\nschema: 1\n" + interaction + "base:\n  merge:\n"
	for _, submission := range accepted {
		if err := joiner.ensure(submission.Model, filepath.Join(joiner.workDir(), "exports", submission.Model)); err != nil {
			return err
		}
		text += fmt.Sprintf("    - model: %s\n      weight: %d\n", submission.Model, submission.Tokens)
	}
	if err := joiner.train(ctx, name, text); err != nil {
		return err
	}
	_, err = joiner.export(ctx, name)
	return err
}

// interaction is the plan's compose interaction block, so merged models chat like trained ones.
func (joiner *Joiner) interaction() (string, error) {
	text, err := joiner.compose("pretrain", "", 0, "tokens: 1", nil)
	if err != nil {
		return "", err
	}
	var compose model.Compose
	if err := yaml.Unmarshal([]byte(text), &compose); err != nil {
		return "", err
	}
	if compose.Interaction == (model.Interaction{}) {
		return "", nil
	}
	encoded, err := yaml.Marshal(map[string]any{"interaction": compose.Interaction})
	return string(encoded), err
}

func copyDirectory(source, target string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	partial := target + ".partial"
	_ = os.RemoveAll(partial)
	if err := os.MkdirAll(partial, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && !strings.HasPrefix(entry.Name(), ".") {
			if err := copyFile(filepath.Join(source, entry.Name()), filepath.Join(partial, entry.Name())); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.Rename(partial, target)
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
