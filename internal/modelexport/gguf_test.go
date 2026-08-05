package modelexport

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/modelquant"
)

func TestByteTokenizerVocabulary(t *testing.T) {
	vocabulary := byteTokenizerVocabulary()
	if len(vocabulary) != 259 {
		t.Fatalf("vocabulary has %d entries, want 259", len(vocabulary))
	}
	if vocabulary[0] != "<pad>" || vocabulary[1] != "<bos>" || vocabulary[2] != "<eos>" {
		t.Fatalf("unexpected special tokens: %#v", vocabulary[:3])
	}
	seen := make(map[string]bool, len(vocabulary))
	for _, token := range vocabulary {
		if seen[token] {
			t.Fatalf("duplicate token %q", token)
		}
		seen[token] = true
	}
}

func TestReleaseBOMEmbedsCompactCalibrationEvidence(t *testing.T) {
	evidence := json.RawMessage(`{"kind":"openwaldo-bom","schema":1,"subject":"quantization-calibration","shards":[{"sha256":"abc"}]}`)
	bom := releaseBOM{Kind: "openwaldo-bom", Schema: 1, Subject: "model-release", Quantization: &releaseQuantization{
		Requested: "4", Resolved: "Q4_K_M", Profile: modelquant.Profile,
		Calibration: &Calibration{TextPath: "/private/sample.txt", Profile: "sample-schema-1", SampledTokens: 100, Evidence: evidence},
	}}
	data, err := json.Marshal(bom)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	quantization := decoded["quantization"].(map[string]any)
	calibration := quantization["calibration"].(map[string]any)
	if calibration["sampled_tokens"] != float64(100) || calibration["evidence"].(map[string]any)["subject"] != "quantization-calibration" {
		t.Fatalf("calibration = %+v", calibration)
	}
	if _, present := calibration["TextPath"]; present || bytes.Contains(data, []byte("/private/sample.txt")) {
		t.Fatalf("private calibration path leaked into BOM: %s", data)
	}
}

func TestGGUFTensorNameAndPermutation(t *testing.T) {
	architecture := model.Architecture{AttentionHeads: 2, KeyValueHeads: 1}
	name, heads, err := ggufTensorName("layers.3.attention.q_proj.weight", architecture)
	if err != nil {
		t.Fatal(err)
	}
	if name != "blk.3.attn_q.weight" || heads != 2 {
		t.Fatalf("mapped to %q with %d heads", name, heads)
	}

	source := bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	var output bytes.Buffer
	tensor := ggufTensor{SourceName: "q", Shape: []uint64{8, 1}, Bytes: 8, Permute: 2}
	if err := writePermutedTensor(&output, source, 0, tensor); err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 2, 1, 3, 4, 6, 5, 7}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("permuted bytes = %v, want %v", output.Bytes(), want)
	}
}

func TestWriteTensorAsF32(t *testing.T) {
	var source bytes.Buffer
	for _, bits := range []uint16{0x3f80, 0xc020} { // BF16 1.0 and -2.5.
		if err := binary.Write(&source, binary.LittleEndian, bits); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	tensor := ggufTensor{SourceName: "norm", SourceType: "BF16", SourceBytes: 4, Bytes: 8}
	if err := writeTensorAsF32(&output, bytes.NewReader(source.Bytes()), 0, tensor); err != nil {
		t.Fatal(err)
	}
	got := []float32{
		math.Float32frombits(binary.LittleEndian.Uint32(output.Bytes()[0:4])),
		math.Float32frombits(binary.LittleEndian.Uint32(output.Bytes()[4:8])),
	}
	if got[0] != 1 || got[1] != -2.5 {
		t.Fatalf("converted values = %v", got)
	}
}

func TestFloat16ToFloat32(t *testing.T) {
	tests := map[uint16]float32{0x0000: 0, 0x3c00: 1, 0xc100: -2.5, 0x7c00: float32(math.Inf(1))}
	for input, want := range tests {
		if got := float16ToFloat32(input); got != want {
			t.Errorf("float16ToFloat32(%#x) = %v, want %v", input, got, want)
		}
	}
}
