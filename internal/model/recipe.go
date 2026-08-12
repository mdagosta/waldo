// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package model owns model compose files, immutable architecture identity, build
// plans, model/run BOMs, and durable lifecycle state.
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"

	"github.com/openwaldo/waldo/internal/training"
	"gopkg.in/yaml.v3"
)

const ComposeSchema = 1

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type Compose struct {
	Kind         string       `json:"kind" yaml:"kind"`
	Schema       int          `json:"schema" yaml:"schema"`
	Base         *ComposeBase `json:"base,omitempty" yaml:"base,omitempty"`
	Architecture Architecture `json:"architecture" yaml:"architecture"`
	Stages       []Stage      `json:"stages" yaml:"stages"`
}

type ComposeBase struct {
	Model        string `json:"model" yaml:"model"`
	OriginSHA256 string `json:"origin_sha256,omitempty" yaml:"origin_sha256,omitempty"`
}

type Architecture struct {
	Family           string    `json:"family" yaml:"family"`
	ContextTokens    uint64    `json:"context_tokens" yaml:"context_tokens"`
	VocabularySize   uint64    `json:"vocabulary_size" yaml:"vocabulary_size"`
	HiddenSize       uint64    `json:"hidden_size" yaml:"hidden_size"`
	IntermediateSize uint64    `json:"intermediate_size" yaml:"intermediate_size"`
	Layers           uint64    `json:"layers" yaml:"layers"`
	AttentionHeads   uint64    `json:"attention_heads" yaml:"attention_heads"`
	KeyValueHeads    uint64    `json:"key_value_heads" yaml:"key_value_heads"`
	Dropout          float64   `json:"dropout,omitempty" yaml:"dropout,omitempty"`
	TieEmbeddings    bool      `json:"tie_embeddings" yaml:"tie_embeddings"`
	ParameterDType   string    `json:"parameter_dtype" yaml:"parameter_dtype"`
	Tokenizer        Tokenizer `json:"tokenizer" yaml:"tokenizer"`
}

type Tokenizer struct {
	Name     string `json:"name" yaml:"name"`
	Revision string `json:"revision" yaml:"revision"`
}

type Stage struct {
	Name       string              `json:"name" yaml:"name"`
	Type       string              `json:"type" yaml:"type"`
	Objective  string              `json:"objective" yaml:"objective"`
	Corpora    []string            `json:"corpora" yaml:"corpora"`
	Parameters training.Parameters `json:"parameters" yaml:"parameters"`
}

type ArchitectureForecast struct {
	ApproximateParameters uint64 `json:"approximate_parameters"`
	ParameterBytes        uint64 `json:"parameter_bytes"`
	Formula               string `json:"formula"`
}

func LoadCompose(path string) (Compose, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Compose{}, "", err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return Compose{}, "", err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var compose Compose
	if err := decoder.Decode(&compose); err != nil {
		return Compose{}, "", fmt.Errorf("%s: %w", absolute, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not allowed")
		}
		return Compose{}, "", fmt.Errorf("%s: %w", absolute, err)
	}
	if err := compose.Validate(); err != nil {
		return Compose{}, "", fmt.Errorf("%s: %w", absolute, err)
	}
	return compose, absolute, nil
}

func IsComposeFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return false, nil
	}
	return header.Kind == "waldo-model-compose", nil
}

func (compose Compose) Validate() error {
	if compose.Kind != "waldo-model-compose" || compose.Schema != ComposeSchema {
		return fmt.Errorf("unsupported model compose identity %q schema %d", compose.Kind, compose.Schema)
	}
	if err := compose.Architecture.Validate(); err != nil {
		return err
	}
	if compose.Base != nil && !validName.MatchString(compose.Base.Model) {
		return fmt.Errorf("base.model must name a locally managed model")
	}
	if len(compose.Stages) == 0 {
		return fmt.Errorf("at least one training stage is required")
	}
	seen := map[string]bool{}
	for i, stage := range compose.Stages {
		if !validName.MatchString(stage.Name) || seen[stage.Name] {
			return fmt.Errorf("stage %d has invalid or duplicate name %q", i+1, stage.Name)
		}
		seen[stage.Name] = true
		if stage.Type != "pre-training" && stage.Type != "fine-tuning" && stage.Type != "alignment" && stage.Type != "other" {
			return fmt.Errorf("stage %s has unsupported type %q; use pre-training, fine-tuning, alignment, or other", stage.Name, stage.Type)
		}
		if stage.Objective != "causal-language-modeling" {
			return fmt.Errorf("stage %s has unsupported objective %q", stage.Name, stage.Objective)
		}
		if len(stage.Corpora) == 0 {
			return fmt.Errorf("stage %s requires at least one index path in corpora", stage.Name)
		}
		corpora := make(map[string]bool, len(stage.Corpora))
		for _, corpusPath := range stage.Corpora {
			if corpusPath == "" {
				return fmt.Errorf("stage %s contains an empty corpus index path", stage.Name)
			}
			if corpora[corpusPath] {
				return fmt.Errorf("stage %s contains duplicate corpus path %q", stage.Name, corpusPath)
			}
			corpora[corpusPath] = true
		}
		parameters := stage.Parameters
		resolved, err := training.ResolveParameters(parameters)
		if err != nil {
			return fmt.Errorf("stage %s training parameters: %w", stage.Name, err)
		}
		if resolved.Data.Order == "corpus-weighted-shuffle-v1" {
			for corpusPath := range corpora {
				if parameters.CorpusWeights[corpusPath] == 0 {
					return fmt.Errorf("stage %s corpus_weights does not declare corpus %q", stage.Name, corpusPath)
				}
			}
			for corpusPath := range parameters.CorpusWeights {
				if !corpora[corpusPath] {
					return fmt.Errorf("stage %s corpus_weights declares unselected corpus %q", stage.Name, corpusPath)
				}
			}
		}
		if uint64(parameters.SequenceLength) > compose.Architecture.ContextTokens {
			return fmt.Errorf("stage %s sequence_length exceeds architecture context_tokens", stage.Name)
		}
	}
	return nil
}

func (architecture Architecture) Validate() error {
	if architecture.Family != "decoder-transformer" {
		return fmt.Errorf("unsupported architecture family %q", architecture.Family)
	}
	if architecture.ContextTokens == 0 || architecture.VocabularySize == 0 || architecture.HiddenSize == 0 || architecture.IntermediateSize == 0 || architecture.Layers == 0 || architecture.AttentionHeads == 0 || architecture.KeyValueHeads == 0 {
		return fmt.Errorf("architecture dimensions must be positive")
	}
	if architecture.HiddenSize%architecture.AttentionHeads != 0 || architecture.AttentionHeads%architecture.KeyValueHeads != 0 {
		return fmt.Errorf("architecture heads must divide hidden_size and key_value_heads must divide attention_heads")
	}
	if architecture.Dropout < 0 || architecture.Dropout >= 1 || math.IsNaN(architecture.Dropout) || math.IsInf(architecture.Dropout, 0) {
		return fmt.Errorf("architecture dropout must be finite and in 0..<1")
	}
	if architecture.ParameterDType != "float32" && architecture.ParameterDType != "float16" && architecture.ParameterDType != "bfloat16" {
		return fmt.Errorf("unsupported parameter_dtype %q", architecture.ParameterDType)
	}
	if architecture.Tokenizer.Name == "" || architecture.Tokenizer.Revision == "" {
		return fmt.Errorf("tokenizer name and immutable revision are required")
	}
	_, err := architecture.Forecast()
	return err
}

func (architecture Architecture) Forecast() (ArchitectureForecast, error) {
	embedding, err := multiply(architecture.VocabularySize, architecture.HiddenSize)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	kvWidth, err := multiply(architecture.HiddenSize/architecture.AttentionHeads, architecture.KeyValueHeads)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	hiddenSquared, err := multiply(architecture.HiddenSize, architecture.HiddenSize)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	qAndOutput, err := multiply(2, hiddenSquared)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	kvProjection, err := multiply(architecture.HiddenSize, kvWidth)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	kvProjection, err = multiply(2, kvProjection)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	attention, err := add(qAndOutput, kvProjection)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	mlp, err := multiply(architecture.HiddenSize, architecture.IntermediateSize)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	mlp, err = multiply(3, mlp)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	block, err := add(attention, mlp)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	blocks, err := multiply(architecture.Layers, block)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	normCount, err := multiply(2, architecture.Layers)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	normCount, err = add(normCount, 1)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	norms, err := multiply(normCount, architecture.HiddenSize)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	parameters, err := add(embedding, blocks, norms)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	if !architecture.TieEmbeddings {
		parameters, err = add(parameters, embedding)
		if err != nil {
			return ArchitectureForecast{}, err
		}
	}
	bytesPerParameter := uint64(4)
	if architecture.ParameterDType != "float32" {
		bytesPerParameter = 2
	}
	parameterBytes, err := multiply(parameters, bytesPerParameter)
	if err != nil {
		return ArchitectureForecast{}, err
	}
	return ArchitectureForecast{ApproximateParameters: parameters, ParameterBytes: parameterBytes, Formula: "embedding + decoder projections + gated MLP + norms; biases excluded"}, nil
}

func multiply(left, right uint64) (uint64, error) {
	high, low := bits.Mul64(left, right)
	if high != 0 {
		return 0, fmt.Errorf("architecture resource estimate overflows uint64")
	}
	return low, nil
}

func add(values ...uint64) (uint64, error) {
	var total uint64
	for _, value := range values {
		var carry uint64
		total, carry = bits.Add64(total, value, 0)
		if carry != 0 {
			return 0, fmt.Errorf("architecture resource estimate overflows uint64")
		}
	}
	return total, nil
}

func canonicalHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}
