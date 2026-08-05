// Package modelweights owns the portable tensor-name contract shared by model
// acquisition, training artifacts, and release conversion.
package modelweights

import (
	"fmt"
	"regexp"
	"strings"
)

var waldoLayerTensor = regexp.MustCompile(`^layers\.([0-9]+)\.(.+)$`)
var huggingFaceLayerTensor = regexp.MustCompile(`^model\.layers\.([0-9]+)\.(.+)$`)

var waldoToHuggingFace = map[string]string{
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

func HuggingFaceName(name string) (string, error) {
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
	tail, ok := waldoToHuggingFace[match[2]]
	if !ok {
		return "", fmt.Errorf("unsupported WALDO tensor %q", name)
	}
	return strings.Join([]string{"model.layers", match[1], tail}, "."), nil
}

func WALDOName(name string) (string, error) {
	switch name {
	case "model.embed_tokens.weight":
		return "embedding.weight", nil
	case "model.norm.weight":
		return "norm.weight", nil
	case "lm_head.weight":
		return "output.weight", nil
	}
	match := huggingFaceLayerTensor.FindStringSubmatch(name)
	if match == nil {
		return "", fmt.Errorf("unsupported Hugging Face tensor %q", name)
	}
	for waldo, huggingFace := range waldoToHuggingFace {
		if match[2] == huggingFace {
			return strings.Join([]string{"layers", match[1], waldo}, "."), nil
		}
	}
	return "", fmt.Errorf("unsupported Hugging Face tensor %q", name)
}

func WALDOLayer(name string) (string, string, bool) {
	match := waldoLayerTensor.FindStringSubmatch(name)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}
