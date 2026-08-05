// Package model owns model recipes, immutable architecture identity, build
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

	"github.com/openwaldo/waldo-new/internal/training"
	"gopkg.in/yaml.v3"
)

const RecipeSchema = 1

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type Recipe struct {
	Kind         string       `json:"kind" yaml:"kind"`
	Schema       int          `json:"schema" yaml:"schema"`
	Name         string       `json:"name" yaml:"name"`
	Architecture Architecture `json:"architecture" yaml:"architecture"`
	Stages       []Stage      `json:"stages" yaml:"stages"`
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
	Corpus     string              `json:"corpus" yaml:"corpus"`
	Parameters training.Parameters `json:"parameters" yaml:"parameters"`
}

type ArchitectureForecast struct {
	ApproximateParameters uint64 `json:"approximate_parameters"`
	ParameterBytes        uint64 `json:"parameter_bytes"`
	Formula               string `json:"formula"`
}

func LoadRecipe(path string) (Recipe, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Recipe{}, "", err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return Recipe{}, "", err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var recipe Recipe
	if err := decoder.Decode(&recipe); err != nil {
		return Recipe{}, "", fmt.Errorf("%s: %w", absolute, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not allowed")
		}
		return Recipe{}, "", fmt.Errorf("%s: %w", absolute, err)
	}
	if err := recipe.Validate(); err != nil {
		return Recipe{}, "", fmt.Errorf("%s: %w", absolute, err)
	}
	for i := range recipe.Stages {
		if !filepath.IsAbs(recipe.Stages[i].Corpus) {
			recipe.Stages[i].Corpus = filepath.Join(filepath.Dir(absolute), recipe.Stages[i].Corpus)
		}
		recipe.Stages[i].Corpus = filepath.Clean(recipe.Stages[i].Corpus)
	}
	return recipe, absolute, nil
}

func (recipe Recipe) Validate() error {
	if recipe.Kind != "waldo-model-recipe" || recipe.Schema != RecipeSchema {
		return fmt.Errorf("unsupported model recipe identity %q schema %d", recipe.Kind, recipe.Schema)
	}
	if !validName.MatchString(recipe.Name) {
		return fmt.Errorf("model name must match %s", validName.String())
	}
	if err := recipe.Architecture.Validate(); err != nil {
		return err
	}
	if len(recipe.Stages) == 0 {
		return fmt.Errorf("at least one training stage is required")
	}
	seen := map[string]bool{}
	for i, stage := range recipe.Stages {
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
		if stage.Corpus == "" {
			return fmt.Errorf("stage %s requires an exported corpus OpenWALDO BOM", stage.Name)
		}
		parameters := stage.Parameters
		if parameters.Steps <= 0 || parameters.BatchSize <= 0 || parameters.SequenceLength <= 0 || parameters.LearningRate <= 0 || math.IsNaN(parameters.LearningRate) || math.IsInf(parameters.LearningRate, 0) {
			return fmt.Errorf("stage %s training parameters must be positive", stage.Name)
		}
		if uint64(parameters.SequenceLength) > recipe.Architecture.ContextTokens {
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
