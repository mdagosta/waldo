package training

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/record"
	"github.com/openwaldo/waldo-new/internal/shard"
	"github.com/parquet-go/parquet-go"
)

func TestResolveParametersPinsVersionedDefaultsAndOverrides(t *testing.T) {
	parameters := Parameters{Steps: 1000, BatchSize: 8, SequenceLength: 512, LearningRate: 0.0003, Seed: 42}
	resolved, err := ResolveParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Profile != DefaultProfile || resolved.ProfileSchema != 1 || resolved.Optimizer.Name != "adamw" || resolved.Optimizer.WeightDecay != 0.1 || resolved.Schedule.Name != "cosine" || resolved.Schedule.WarmupSteps != 100 || resolved.Data.Order != "bounded-shuffle-v1" || resolved.Data.Packing != "continuous-eos-v1" || resolved.CheckpointEvery != 500 || resolved.EvaluateEvery != 500 || resolved.PlannedTokenCapacity != 4_096_000 {
		t.Fatalf("resolved = %+v", resolved)
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

func TestWorkerProtocolStreamsBeginRecordsEndAndValidatesOutput(t *testing.T) {
	inputs := []Input{writeTrainingShard(t, []string{"one", "two"})}
	parameters, err := ResolveParameters(Parameters{Steps: 1, BatchSize: 1, SequenceLength: 8, LearningRate: 0.001, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewCanonicalRecordSource(inputs, parameters)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	begin := WorkerBegin{RunID: "run", Stage: "pretrain", Objective: "causal-language-modeling", ArchitectureSHA256: strings.Repeat("a", 64), Architecture: json.RawMessage(`{"family":"decoder-transformer"}`), Parameters: parameters}
	if err := WriteWorkerInput(context.Background(), &encoded, begin, source); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	scanner := bufio.NewScanner(bytes.NewReader(encoded.Bytes()))
	for scanner.Scan() {
		var frame WorkerInputFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, frame.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"begin", "record", "record", "end"}) {
		t.Fatalf("frame kinds = %v", kinds)
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
	var encoded bytes.Buffer
	writer := parquet.NewGenericWriter[shard.Row](&encoded)
	rows := make([]shard.Row, 0, len(texts))
	for _, text := range texts {
		rows = append(rows, shard.Row{SHA256: record.TextHash(text), Kind: record.KindPretrain, Text: text, Source: "fixture", License: "CC0-1.0", Tokens: 1})
	}
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
	return Input{Path: path, SHA256: digest, Bytes: int64(len(data))}
}
