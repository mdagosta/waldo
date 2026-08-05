package modelexport

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteHuggingFaceWeightsRenamesHeaderWithoutChangingTensorBytes(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.safetensors")
	destination := filepath.Join(t.TempDir(), "model.safetensors")
	mlxDestination := filepath.Join(t.TempDir(), "model.safetensors")
	header, err := json.Marshal(map[string]any{
		"__metadata__":                     map[string]string{"format": "openwaldo"},
		"embedding.weight":                 map[string]any{"dtype": "F32", "shape": []int{1}, "data_offsets": []int{0, 4}},
		"layers.0.attention.q_proj.weight": map[string]any{"dtype": "F32", "shape": []int{1}, "data_offsets": []int{4, 8}},
		"layers.0.ffn_norm.weight":         map[string]any{"dtype": "F32", "shape": []int{1}, "data_offsets": []int{8, 12}},
		"norm.weight":                      map[string]any{"dtype": "F32", "shape": []int{1}, "data_offsets": []int{12, 16}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for len(header)%8 != 0 {
		header = append(header, ' ')
	}
	data := []byte("0123456789abcdef")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(file, binary.LittleEndian, uint64(len(header))); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rewriteHuggingFaceWeights(source, destination); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	length := binary.LittleEndian.Uint64(output[:8])
	var rewritten map[string]json.RawMessage
	if err := json.Unmarshal(output[8:8+length], &rewritten); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model.embed_tokens.weight", "model.layers.0.self_attn.q_proj.weight", "model.layers.0.post_attention_layernorm.weight", "model.norm.weight"} {
		if _, ok := rewritten[name]; !ok {
			t.Errorf("rewritten header missing %s: %v", name, rewritten)
		}
	}
	var metadata map[string]string
	if err := json.Unmarshal(rewritten["__metadata__"], &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["format"] != "pt" || metadata["source_format"] != "openwaldo" {
		t.Fatalf("rewritten metadata = %v", metadata)
	}
	if got := output[8+length:]; string(got) != string(data) {
		t.Fatalf("tensor bytes = %q, want %q", got, data)
	}
	if err := rewriteMLXWeights(source, mlxDestination); err != nil {
		t.Fatal(err)
	}
	mlxOutput, err := os.ReadFile(mlxDestination)
	if err != nil {
		t.Fatal(err)
	}
	mlxLength := binary.LittleEndian.Uint64(mlxOutput[:8])
	var mlxHeader map[string]json.RawMessage
	if err := json.Unmarshal(mlxOutput[8:8+mlxLength], &mlxHeader); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mlxHeader["__metadata__"], &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["format"] != "mlx" || metadata["export_format"] != "mlx" {
		t.Fatalf("MLX metadata = %v", metadata)
	}
	if got := mlxOutput[8+mlxLength:]; string(got) != string(data) {
		t.Fatalf("MLX tensor bytes = %q, want %q", got, data)
	}
}

func TestHuggingFaceTensorNameRejectsUnknownWeights(t *testing.T) {
	if _, err := huggingFaceTensorName("layers.0.unknown.weight"); err == nil {
		t.Fatal("unknown tensor was accepted")
	}
}
