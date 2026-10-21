// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package modelexport

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	waldotokenizer "github.com/openwaldo/waldo/internal/tokenizer"
	"github.com/openwaldo/waldo/internal/training"
)

type exportTokenizerFixture struct {
	Kind   string `json:"kind"`
	Schema int    `json:"schema"`
	training.TokenizerSpec
}

func TestExportTokenizerArtifactPreservesTrainedBPE(t *testing.T) {
	artifact, path := writeExportTokenizerFixture(t)
	data, spec, err := exportTokenizerArtifact(path, uint64(artifact.VocabularySize))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Artifact == nil || spec.Revision != artifact.Revision || spec.VocabularySize != artifact.VocabularySize {
		t.Fatalf("exported tokenizer spec = %+v", spec)
	}
	var exported exportTokenizerFixture
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Artifact == nil || !reflect.DeepEqual(*exported.Artifact, artifact) {
		t.Fatalf("exported tokenizer artifact differs from trained artifact")
	}
	if _, _, err := exportTokenizerArtifact(path, uint64(artifact.VocabularySize+1)); err == nil || !strings.Contains(err.Error(), "does not match architecture") {
		t.Fatalf("vocabulary mismatch error = %v", err)
	}
}

func TestHuggingFaceTrainedBPETokenizerMatchesNativeCodec(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	artifact, artifactPath := writeExportTokenizerFixture(t)
	modulePath := filepath.Join(t.TempDir(), "tokenization_openwaldo.py")
	if err := os.WriteFile(modulePath, []byte(huggingFaceTokenizerPython()), 0o644); err != nil {
		t.Fatal(err)
	}
	codec, err := artifact.Codec()
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{
		"Once upon a time, a little fox ran home.",
		"  café—stars\nshine!  ",
		strings.Repeat("elementary ", 12),
	}
	want := make([][]int, len(texts))
	for index, text := range texts {
		want[index] = codec.Encode(text)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	textsJSON, err := json.Marshal(texts)
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, json, sys, types
module = types.ModuleType("transformers")
class PreTrainedTokenizer:
    def __init__(self, **kwargs): pass
module.PreTrainedTokenizer = PreTrainedTokenizer
sys.modules["transformers"] = module
spec = importlib.util.spec_from_file_location("tokenization_openwaldo", sys.argv[1])
loaded = importlib.util.module_from_spec(spec)
spec.loader.exec_module(loaded)
tokenizer = loaded.OpenWALDOTokenizer(vocab_file=sys.argv[2])
encoded = [tokenizer._encode_text(value) for value in json.loads(sys.argv[3])]
converted = [[tokenizer._convert_token_to_id(tokenizer._token_name(index)) for index in values] for values in encoded]
print(json.dumps({"encoded": encoded, "converted": converted}))
`
	output, err := exec.Command(python, "-c", script, modulePath, artifactPath, string(textsJSON)).CombinedOutput()
	if err != nil {
		t.Fatalf("execute exported tokenizer: %v\n%s", err, output)
	}
	var got struct {
		Encoded   [][]int `json:"encoded"`
		Converted [][]int `json:"converted"`
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("decode exported tokenizer result: %v\n%s", err, output)
	}
	if !reflect.DeepEqual(got.Encoded, want) || !reflect.DeepEqual(got.Converted, want) {
		t.Fatalf("exported tokenizer IDs = %+v, want %v", got, want)
	}
	if !json.Valid(wantJSON) {
		t.Fatal("native token IDs are not valid JSON")
	}
}

func writeExportTokenizerFixture(t *testing.T) (waldotokenizer.Artifact, string) {
	t.Helper()
	artifact, err := waldotokenizer.TrainByteBPE(
		[]waldotokenizer.Sample{
			{ID: "one", Text: strings.Repeat("Once upon a time, a little fox ran home. ", 20)},
			{ID: "two", Text: strings.Repeat("The bright stars shine over the quiet town. ", 20)},
		},
		320,
		4096,
		strings.Repeat("a", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	payload := exportTokenizerFixture{
		Kind: "waldo-tokenizer", Schema: 1,
		TokenizerSpec: training.TokenizerSpec{
			Name: artifact.Name, Revision: artifact.Revision, VocabularySize: artifact.VocabularySize,
			PadID: artifact.PadID, BOSID: artifact.BOSID, EOSID: artifact.EOSID, Artifact: &artifact,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tokenizer.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return artifact, path
}
