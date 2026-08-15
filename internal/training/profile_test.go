// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
	"github.com/openwaldo/waldo/internal/record"
	"github.com/openwaldo/waldo/internal/shard"
	"github.com/parquet-go/parquet-go"
)

func TestResolveParametersPinsVersionedDefaultsAndOverrides(t *testing.T) {
	parameters := Parameters{Steps: 1000, BatchSize: 8, SequenceLength: 512, LearningRate: 0.0003, Seed: 42}
	resolved, err := ResolveParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Profile != DefaultProfile || resolved.ProfileSchema != 1 || resolved.Epochs != 1 || resolved.Optimizer.Name != "adamw" || resolved.Optimizer.WeightDecay != 0.1 || resolved.Schedule.Name != "cosine" || resolved.Schedule.WarmupSteps != 100 || resolved.Data.Order != "bounded-shuffle-v1" || resolved.Data.Packing != "continuous-eos-v1" || resolved.Evaluation == nil || resolved.Evaluation.Selection != "lowest-sha256-v1" || resolved.Evaluation.Fraction != 0.01 || resolved.Evaluation.MaxRecords != 256 || resolved.Evaluation.MaxBytes != 1024*1024 || resolved.CheckpointEvery != 500 || resolved.EvaluateEvery != 500 || resolved.PlannedTokenCapacity != 4_096_000 {
		t.Fatalf("resolved = %+v", resolved)
	}
	encoded, err := json.Marshal(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"epochs":1`)) {
		t.Fatalf("resolved parameters do not persist the default epoch: %s", encoded)
	}
	zeroFloat := 0.0
	zeroInt := int64(0)
	buffer := 7
	bufferBytes := int64(4096)
	parameters.WeightDecay = &zeroFloat
	parameters.WarmupSteps = &zeroInt
	parameters.CheckpointEvery = &zeroInt
	parameters.EvaluateEvery = &zeroInt
	parameters.ShuffleBufferRecords = &buffer
	parameters.ShuffleBufferBytes = &bufferBytes
	resolved, err = ResolveParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Optimizer.WeightDecay != 0 || resolved.Schedule.WarmupSteps != 0 || resolved.CheckpointEvery != 0 || resolved.EvaluateEvery != 0 || resolved.Data.ShuffleBufferRecords != 7 || resolved.Data.ShuffleBufferBytes != 4096 {
		t.Fatalf("overrides = %+v", resolved)
	}
	bad := parameters
	bad.Profile = "unknown"
	if _, err := ResolveParameters(bad); err == nil {
		t.Fatal("unknown profile accepted")
	}
	bad = parameters
	bad.Epochs = -1
	if _, err := ResolveParameters(bad); err == nil {
		t.Fatal("negative epochs accepted")
	}
}

func TestBalancedProfilePinsCorpusBalancedDataAndEvaluation(t *testing.T) {
	resolved, err := ResolveParameters(Parameters{Profile: BalancedProfile, Steps: 10, BatchSize: 2, SequenceLength: 8, LearningRate: 0.001, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProfileSchema != 1 || resolved.Data.Order != "corpus-balanced-shuffle-v1" || resolved.Evaluation == nil || resolved.Evaluation.Selection != "stratified-lowest-sha256-v1" {
		t.Fatalf("balanced profile = %+v", resolved)
	}
}

func TestWeightedProfilePinsDeclaredCorpusWeights(t *testing.T) {
	parameters := Parameters{Profile: WeightedProfile, Steps: 10, BatchSize: 2, SequenceLength: 8, LearningRate: 0.001, Seed: 42, CorpusWeights: map[string]uint64{"corpus-a": 3, "corpus-b": 1}}
	resolved, err := ResolveParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProfileSchema != 1 || resolved.Data.Order != "corpus-weighted-shuffle-v1" || !reflect.DeepEqual(resolved.Data.CorpusWeights, parameters.CorpusWeights) {
		t.Fatalf("weighted profile = %+v", resolved)
	}
	parameters.Profile = BalancedProfile
	if _, err := ResolveParameters(parameters); err == nil {
		t.Fatal("balanced profile accepted corpus_weights")
	}
}

func TestNumberedProfileAliasesResolveToCanonicalNames(t *testing.T) {
	for legacy, canonical := range map[string]string{
		"causal-pretrain-v1": ShuffledProfile,
		"causal-pretrain-v2": BalancedProfile,
		"causal-pretrain-v3": WeightedProfile,
	} {
		parameters := Parameters{Profile: legacy, Steps: 10, BatchSize: 2, SequenceLength: 8, LearningRate: 0.001, Seed: 42}
		if canonical == WeightedProfile {
			parameters.CorpusWeights = map[string]uint64{"corpus": 1}
		}
		resolved, err := ResolveParameters(parameters)
		if err != nil {
			t.Fatalf("alias %s: %v", legacy, err)
		}
		if resolved.Profile != canonical || resolved.ProfileSchema != ProfileSchema {
			t.Fatalf("alias %s resolved as %s schema %d", legacy, resolved.Profile, resolved.ProfileSchema)
		}
	}
	legacy := ResolvedParameters{Profile: "causal-pretrain-v3", ProfileSchema: 3}
	canonical := NormalizeResolvedParameters(legacy)
	if canonical.Profile != WeightedProfile || canonical.ProfileSchema != ProfileSchema {
		t.Fatalf("persisted profile normalization = %+v", canonical)
	}
}

func TestRecordPartitionPinsAndExcludesHeldOutRecords(t *testing.T) {
	var texts []string
	for index := 0; index < 100; index++ {
		texts = append(texts, fmt.Sprintf("record-%03d", index))
	}
	inputs := []Input{writeTrainingShard(t, texts)}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 16, LearningRate: 0.001, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewRecordPartition(inputs, parameters)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRecordPartition(inputs, parameters)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Evaluation, second.Evaluation) || first.Evaluation.Records != 1 || first.Evaluation.TokenTargets == 0 || len(first.Evaluation.SHA256) != 64 {
		t.Fatalf("evaluation evidence = %+v / %+v", first.Evaluation, second.Evaluation)
	}
	trainingSource, err := first.TrainingRecords()
	if err != nil {
		t.Fatal(err)
	}
	trainingIDs := map[string]bool{}
	if err := trainingSource.Stream(context.Background(), func(value Record) error { trainingIDs[value.SelectionID] = true; return nil }); err != nil {
		t.Fatal(err)
	}
	evaluationIDs := map[string]bool{}
	if err := first.EvaluationRecords().Stream(context.Background(), func(value Record) error { evaluationIDs[value.SelectionID] = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(trainingIDs) != 99 || len(evaluationIDs) != 1 {
		t.Fatalf("partition sizes = training %d, evaluation %d", len(trainingIDs), len(evaluationIDs))
	}
	for key := range evaluationIDs {
		if trainingIDs[key] {
			t.Fatalf("held-out record %s appears in training", key)
		}
	}
	if targets, err := first.TrainingByteTargets(context.Background()); err != nil || targets <= 0 {
		t.Fatalf("training targets = %d, err = %v", targets, err)
	}
}

func TestRecordFiltersApplyToPartitionTargetsAndTrainingStream(t *testing.T) {
	input := writeTrainingRows(t, []shard.Row{
		{SHA256: record.TextHash("keep"), Kind: record.KindPretrain, Text: "keep", Source: "source-a", SourceName: "project-a", License: "CC-BY-4.0", Lang: "en", Date: "2024", Tokens: 1},
		{SHA256: record.TextHash("wrong language"), Kind: record.KindPretrain, Text: "wrong language", Source: "source-a", SourceName: "project-a", License: "CC-BY-4.0", Lang: "fr", Date: "2024", Tokens: 1},
		{SHA256: record.TextHash("wrong license"), Kind: record.KindPretrain, Text: "wrong license", Source: "source-a", SourceName: "project-a", License: "GPL-2.0-only", Lang: "en", Date: "2024", Tokens: 1},
	})
	input.Corpus = "example"
	input.RecordFilter = &corpus.RecordFilterPolicy{
		Schema:  corpus.RecordFilterSchema,
		Global:  &corpus.RecordFilter{Languages: &corpus.ValueFilter{Include: []string{"en"}}},
		Corpora: map[string]corpus.RecordFilter{"example": {Licenses: &corpus.ValueFilter{Include: []string{"CC-BY-*"}}, Date: &corpus.DateFilter{From: "2020"}}},
	}
	zeroFraction := 0.0
	zeroRecords := 0
	zeroBytes := int64(0)
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 42, EvaluationFraction: &zeroFraction, EvaluationMaxRecords: &zeroRecords, EvaluationMaxBytes: &zeroBytes})
	if err != nil {
		t.Fatal(err)
	}
	partition, err := NewRecordPartition([]Input{input}, parameters)
	if err != nil {
		t.Fatal(err)
	}
	source, err := partition.TrainingRecords()
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	if err := source.Stream(context.Background(), func(value Record) error {
		texts = append(texts, value.Text)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(texts, []string{"keep"}) {
		t.Fatalf("filtered training texts = %v", texts)
	}
	if targets, err := CountByteTargets(context.Background(), []Input{input}); err != nil || targets != int64(len("keep")) {
		t.Fatalf("filtered byte targets = %d, err = %v", targets, err)
	}
}

type countingTokenCodec struct {
	counts int
}

func (codec *countingTokenCodec) Count(text string) int {
	codec.counts++
	return len([]byte(text))
}

func (*countingTokenCodec) Encode(text string) []int   { return byteCodec{}.Encode(text) }
func (*countingTokenCodec) Decode(tokens []int) string { return byteCodec{}.Decode(tokens) }

func TestRecordPartitionTokenizesAndCachesOnlySelectedRecords(t *testing.T) {
	texts := make([]string, 1000)
	for index := range texts {
		texts[index] = fmt.Sprintf("record-%04d", index)
	}
	input := writeTrainingShard(t, texts)
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 16, LearningRate: 0.001, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	codec := &countingTokenCodec{}
	partition, err := NewRecordPartitionContextWithTokenizer(context.Background(), []Input{input}, parameters, codec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if codec.counts != int(partition.Evaluation.Records) || codec.counts != 10 {
		t.Fatalf("tokenizer calls = %d, evaluation records = %d", codec.counts, partition.Evaluation.Records)
	}
	if err := os.Remove(input.Path); err != nil {
		t.Fatal(err)
	}
	var cached int
	if err := partition.EvaluationRecords().Stream(context.Background(), func(Record) error {
		cached++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if cached != codec.counts {
		t.Fatalf("cached evaluation records = %d, want %d", cached, codec.counts)
	}
}

func TestRecordPartitionHonorsCanceledContext(t *testing.T) {
	inputs := []Input{writeTrainingShard(t, []string{"one", "two"})}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewRecordPartitionContext(ctx, inputs, parameters, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("partition cancellation error = %v", err)
	}
}

func TestRecordPartitionReportsScanProgress(t *testing.T) {
	inputs := []Input{
		writeTrainingShard(t, []string{"one", "two"}),
		writeTrainingShard(t, []string{"three"}),
	}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	var events []PartitionProgress
	if _, err := NewRecordPartitionWithProgress(inputs, parameters, func(event PartitionProgress) {
		events = append(events, event)
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("progress events = %+v", events)
	}
	last := events[len(events)-1]
	if last.CurrentShard != 2 || last.TotalShards != 2 || last.Records != 3 || last.Bytes != last.TotalBytes || last.TotalBytes <= 0 {
		t.Fatalf("final progress = %+v", last)
	}
}

func TestCanonicalRecordSourceIsDeterministicAndComplete(t *testing.T) {
	inputs := []Input{
		writeTrainingShard(t, []string{"one", "two", "three"}),
		writeTrainingShard(t, []string{"four", "five", "six"}),
	}
	buffer := 2
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 42, ShuffleBufferRecords: &buffer})
	if err != nil {
		t.Fatal(err)
	}
	first := collectRecords(t, inputs, parameters)
	second := collectRecords(t, inputs, parameters)
	if !reflect.DeepEqual(first, second) || len(first) != 6 {
		t.Fatalf("first = %v, second = %v", first, second)
	}
	parameters.Seed = 43
	third := collectRecords(t, inputs, parameters)
	if reflect.DeepEqual(first, third) {
		t.Fatalf("different seed produced the same order: %v", first)
	}
	sorted := append([]string(nil), first...)
	other := append([]string(nil), third...)
	sort.Strings(sorted)
	sort.Strings(other)
	if !reflect.DeepEqual(sorted, other) {
		t.Fatalf("record sets differ: %v, %v", sorted, other)
	}
}

func TestBalancedRecordSourceInterleavesDeclaredCorpora(t *testing.T) {
	first := writeTrainingShard(t, []string{"a1", "a2", "a3", "a4"})
	first.Corpus = "corpus-a"
	second := writeTrainingShard(t, []string{"b1", "b2", "b3", "b4"})
	second.Corpus = "corpus-b"
	parameters, err := ResolveParameters(Parameters{Profile: BalancedProfile, Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewCanonicalRecordSource([]Input{first, second}, parameters)
	if err != nil {
		t.Fatal(err)
	}
	var corpora []string
	if err := source.Stream(context.Background(), func(value Record) error {
		corpora = append(corpora, value.Corpus)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"corpus-a", "corpus-b", "corpus-a", "corpus-b", "corpus-a", "corpus-b", "corpus-a", "corpus-b"}
	if !reflect.DeepEqual(corpora, want) {
		t.Fatalf("corpus order = %v, want %v", corpora, want)
	}
}

func TestBalancedRecordSourceAccountsForTokenLength(t *testing.T) {
	first := writeTrainingShard(t, []string{strings.Repeat("a", 20), strings.Repeat("a", 20)})
	first.Corpus = "corpus-a"
	second := writeTrainingShard(t, []string{"b", "b", "b", "b", "b", "b"})
	second.Corpus = "corpus-b"
	parameters, err := ResolveParameters(Parameters{Profile: BalancedProfile, Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewCanonicalRecordSource([]Input{first, second}, parameters)
	if err != nil {
		t.Fatal(err)
	}
	var corpora []string
	if err := source.Stream(context.Background(), func(value Record) error {
		corpora = append(corpora, value.Corpus)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(corpora) < 4 || !reflect.DeepEqual(corpora[:4], []string{"corpus-a", "corpus-b", "corpus-b", "corpus-b"}) {
		t.Fatalf("token-balanced prefix = %v", corpora)
	}
}

func TestWeightedRecordSourceHonorsTokenRatios(t *testing.T) {
	first := writeTrainingShard(t, []string{"a1", "a2", "a3", "a4"})
	first.Corpus = "corpus-a"
	second := writeTrainingShard(t, []string{"b1", "b2", "b3", "b4"})
	second.Corpus = "corpus-b"
	parameters, err := ResolveParameters(Parameters{Profile: WeightedProfile, Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 42, CorpusWeights: map[string]uint64{"corpus-a": 3, "corpus-b": 1}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewCanonicalRecordSource([]Input{first, second}, parameters)
	if err != nil {
		t.Fatal(err)
	}
	var corpora []string
	if err := source.Stream(context.Background(), func(value Record) error {
		corpora = append(corpora, value.Corpus)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(corpora) < 5 || !reflect.DeepEqual(corpora[:5], []string{"corpus-a", "corpus-b", "corpus-a", "corpus-a", "corpus-a"}) {
		t.Fatalf("weighted corpus prefix = %v", corpora)
	}
}

func TestBalancedEvaluationIncludesEveryCorpus(t *testing.T) {
	first := writeTrainingShard(t, []string{"a1", "a2", "a3", "a4"})
	first.Corpus = "corpus-a"
	second := writeTrainingShard(t, []string{"b1", "b2", "b3", "b4"})
	second.Corpus = "corpus-b"
	fraction := 0.5
	maximum := 4
	parameters, err := ResolveParameters(Parameters{Profile: BalancedProfile, Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 42, EvaluationFraction: &fraction, EvaluationMaxRecords: &maximum})
	if err != nil {
		t.Fatal(err)
	}
	partition, err := NewRecordPartition([]Input{first, second}, parameters)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	if err := partition.EvaluationRecords().Stream(context.Background(), func(value Record) error {
		seen[value.Corpus]++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen["corpus-a"] != 2 || seen["corpus-b"] != 2 {
		t.Fatalf("stratified evaluation = %v", seen)
	}
}

func TestCanonicalRecordSourceHonorsByteBound(t *testing.T) {
	inputs := []Input{writeTrainingShard(t, []string{"one", "two", "three"})}
	bufferRecords := 100
	bufferBytes := int64(1)
	parameters, err := ResolveParameters(Parameters{
		Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001,
		Seed: 42, ShuffleBufferRecords: &bufferRecords, ShuffleBufferBytes: &bufferBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectRecords(t, inputs, parameters)
	want := []string{record.TextHash("one"), record.TextHash("two"), record.TextHash("three")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("byte-bounded order = %v, want %v", got, want)
	}
}

func TestCountByteTargetsUsesUTF8BytesAndEOS(t *testing.T) {
	inputs := []Input{writeTrainingShard(t, []string{"A", "é"})}
	// [A, EOS, 0xc3, 0xa9, EOS] has four next-token targets.
	targets, err := CountByteTargets(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if targets != 4 {
		t.Fatalf("byte targets = %d, want 4", targets)
	}
}

func TestByteTargetsAndRecordSourceRepeatExactEpochs(t *testing.T) {
	inputs := []Input{writeTrainingShard(t, []string{"A", "é"})}
	oneEpoch, err := CountByteTargets(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := ByteTargetsForEpochs(oneEpoch, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Each epoch contains five tokens including EOS. Across two continuous
	// epochs only the first token lacks a prediction target: 5*2-1 = 9.
	if targets != 9 {
		t.Fatalf("two-epoch targets = %d, want 9", targets)
	}
	parameters, err := ResolveParameters(Parameters{Epochs: 2, Steps: 1, BatchSize: 1, SequenceLength: 16, LearningRate: 0.001, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	got := collectRecords(t, inputs, parameters)
	if len(got) != 4 {
		t.Fatalf("two-epoch record count = %d, want 4: %v", len(got), got)
	}
}

func TestWorkerProtocolStreamsBeginRecordsEndAndValidatesOutput(t *testing.T) {
	inputs := []Input{writeTrainingShard(t, []string{"one", "two"})}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	begin := WorkerBegin{RunID: "run", Stage: "pretrain", Objective: "causal-language-modeling", ArchitectureSHA256: strings.Repeat("a", 64), Architecture: json.RawMessage(`{"family":"decoder-transformer"}`), Parameters: parameters}
	partition, err := NewRecordPartition(inputs, parameters)
	if err != nil {
		t.Fatal(err)
	}
	trainingSource, err := partition.TrainingRecords()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteWorkerInput(context.Background(), &encoded, begin, trainingSource, partition.EvaluationRecords()); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var decodedBegin WorkerBegin
	scanner := bufio.NewScanner(bytes.NewReader(encoded.Bytes()))
	for scanner.Scan() {
		var frame WorkerInputFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, frame.Kind)
		if frame.Begin != nil {
			decodedBegin = *frame.Begin
		}
	}
	if !reflect.DeepEqual(kinds, []string{"begin", "evaluation_record", "record", "end"}) {
		t.Fatalf("frame kinds = %v", kinds)
	}
	if !reflect.DeepEqual(decodedBegin, begin) {
		t.Fatalf("worker begin lost compose settings:\n got  %+v\n want %+v", decodedBegin, begin)
	}

	output := "{\"kind\":\"event\",\"schema\":1,\"event\":{\"kind\":\"progress\",\"step\":1}}\n" +
		"{\"kind\":\"complete\",\"schema\":1,\"observation\":{\"simulated\":false,\"steps\":1,\"consumed_tokens\":8,\"artifacts\":[]}}\n"
	count := 0
	if err := ReadWorkerOutput(strings.NewReader(output), func(WorkerOutputFrame) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("output frames = %d", count)
	}
	if err := ReadWorkerOutput(strings.NewReader(`{"kind":"complete","schema":2,"observation":{}}`+"\n"), func(WorkerOutputFrame) error { return nil }); err == nil {
		t.Fatal("unsupported protocol schema accepted")
	}
	if err := ReadWorkerOutput(strings.NewReader(`{"kind":"event","schema":1,"event":{"kind":"mystery"}}`+"\n"), func(WorkerOutputFrame) error { return nil }); err == nil {
		t.Fatal("unsupported event kind accepted")
	}
}

func TestWorkerInputStopsTrainingRecordsAfterTargetSignal(t *testing.T) {
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	begin := WorkerBegin{RunID: "run", Stage: "pretrain", Objective: "causal-language-modeling", Parameters: parameters}
	stop := make(chan struct{})
	streamed := 0
	records := recordSourceFunc(func(_ context.Context, consume func(Record) error) error {
		for position := 0; position < 100; position++ {
			if err := consume(Record{ID: fmt.Sprintf("record-%d", position), Text: "training text"}); err != nil {
				return err
			}
			streamed++
			if position == 0 {
				close(stop)
			}
		}
		return nil
	})
	var encoded bytes.Buffer
	if err := writeWorkerInputUntil(context.Background(), &encoded, begin, records, nil, stop); err != nil {
		t.Fatal(err)
	}
	if streamed != 1 {
		t.Fatalf("streamed %d records after target signal, want 1", streamed)
	}
	if !strings.Contains(encoded.String(), `"kind":"end"`) {
		t.Fatalf("worker stream did not terminate cleanly: %s", encoded.String())
	}
}

func collectRecords(t *testing.T, inputs []Input, parameters ResolvedParameters) []string {
	t.Helper()
	source, err := NewCanonicalRecordSource(inputs, parameters)
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	if err := source.Stream(context.Background(), func(record Record) error {
		result = append(result, record.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func writeTrainingShard(t *testing.T, texts []string) Input {
	t.Helper()
	rows := make([]shard.Row, 0, len(texts))
	for _, text := range texts {
		rows = append(rows, shard.Row{SHA256: record.TextHash(text), Kind: record.KindPretrain, Text: text, Source: "fixture", License: "CC0-1.0", Tokens: 1})
	}
	return writeTrainingRows(t, rows)
}

func writeTrainingRows(t *testing.T, rows []shard.Row) Input {
	t.Helper()
	var encoded bytes.Buffer
	writer := parquet.NewGenericWriter[shard.Row](&encoded)
	if _, err := writer.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	digestArray := sha256.Sum256(data)
	digest := hex.EncodeToString(digestArray[:])
	path := filepath.Join(t.TempDir(), digest+".parquet")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return Input{Path: path, SHA256: digest, Bytes: int64(len(data)), Records: int64(len(rows))}
}
