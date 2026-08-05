package modelexport

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var waldoLayerTensor = regexp.MustCompile(`^layers\.([0-9]+)\.(.+)$`)

func rewriteHuggingFaceWeights(source, destination string) error {
	return rewriteLlamaWeights(source, destination, "pt", "huggingface")
}

func rewriteMLXWeights(source, destination string) error {
	return rewriteLlamaWeights(source, destination, "mlx", "mlx")
}

func rewriteLlamaWeights(source, destination, containerFormat, exportFormat string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	var headerBytes [8]byte
	if _, err := io.ReadFull(input, headerBytes[:]); err != nil {
		return fmt.Errorf("read Safetensors header length: %w", err)
	}
	headerLength := binary.LittleEndian.Uint64(headerBytes[:])
	if headerLength == 0 || headerLength > 1<<30 {
		return fmt.Errorf("invalid Safetensors header length %d", headerLength)
	}
	header := make([]byte, headerLength)
	if _, err := io.ReadFull(input, header); err != nil {
		return fmt.Errorf("read Safetensors header: %w", err)
	}
	var tensors map[string]json.RawMessage
	if err := json.Unmarshal(header, &tensors); err != nil {
		return fmt.Errorf("decode Safetensors header: %w", err)
	}
	rewritten := make(map[string]json.RawMessage, len(tensors))
	for name, descriptor := range tensors {
		target := name
		if name == "__metadata__" {
			var metadata map[string]string
			if err := json.Unmarshal(descriptor, &metadata); err != nil {
				return fmt.Errorf("decode Safetensors metadata: %w", err)
			}
			metadata["source_format"] = metadata["format"]
			metadata["format"] = containerFormat
			metadata["export_format"] = exportFormat
			descriptor, err = json.Marshal(metadata)
			if err != nil {
				return err
			}
		} else {
			target, err = huggingFaceTensorName(name)
			if err != nil {
				return err
			}
		}
		if _, exists := rewritten[target]; exists {
			return fmt.Errorf("Safetensors tensor mapping collides at %q", target)
		}
		rewritten[target] = descriptor
	}
	encoded, err := json.Marshal(rewritten)
	if err != nil {
		return err
	}
	for len(encoded)%8 != 0 {
		encoded = append(encoded, ' ')
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = os.Remove(destination)
		}
	}()
	binary.LittleEndian.PutUint64(headerBytes[:], uint64(len(encoded)))
	if _, err := output.Write(headerBytes[:]); err != nil {
		return err
	}
	if _, err := output.Write(encoded); err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	committed = true
	return nil
}

func huggingFaceTensorName(name string) (string, error) {
	switch name {
	case "embedding.weight":
		return "model.embed_tokens.weight", nil
	case "norm.weight":
		return "model.norm.weight", nil
	case "output.weight":
		return "lm_head.weight", nil
	}
	match := waldoLayerTensor.FindStringSubmatch(name)
	if match == nil {
		return "", fmt.Errorf("unsupported WALDO tensor %q", name)
	}
	replacements := map[string]string{
		"attention.q_proj.weight":  "self_attn.q_proj.weight",
		"attention.k_proj.weight":  "self_attn.k_proj.weight",
		"attention.v_proj.weight":  "self_attn.v_proj.weight",
		"attention.o_proj.weight":  "self_attn.o_proj.weight",
		"attention_norm.weight":    "input_layernorm.weight",
		"feed_forward.gate.weight": "mlp.gate_proj.weight",
		"feed_forward.up.weight":   "mlp.up_proj.weight",
		"feed_forward.down.weight": "mlp.down_proj.weight",
		"ffn_norm.weight":          "post_attention_layernorm.weight",
	}
	tail, ok := replacements[match[2]]
	if !ok {
		return "", fmt.Errorf("unsupported WALDO tensor %q", name)
	}
	return strings.Join([]string{"model.layers", match[1], tail}, "."), nil
}
