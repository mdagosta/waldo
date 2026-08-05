// Package training is the narrow adapter boundary between WALDO's durable
// model lifecycle and an execution framework.
package training

import (
	"context"

	"github.com/openwaldo/waldo-new/internal/corpus"
)

type Identity struct {
	Name     string `json:"name" yaml:"name"`
	Revision string `json:"revision" yaml:"revision"`
}

type Parameters struct {
	Steps          int64   `json:"steps" yaml:"steps"`
	BatchSize      int64   `json:"batch_size" yaml:"batch_size"`
	SequenceLength int64   `json:"sequence_length" yaml:"sequence_length"`
	LearningRate   float64 `json:"learning_rate" yaml:"learning_rate"`
	Seed           uint64  `json:"seed" yaml:"seed"`
}

type Input struct {
	Path   string
	SHA256 string
	Bytes  int64
}

type Request struct {
	RunID              string
	ArchitectureSHA256 string
	BOM                corpus.BOM
	Inputs             []Input
	Parameters         Parameters
	OutputPath         string
	ArtifactPath       string
}

type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Observation struct {
	Simulated      bool       `json:"simulated"`
	Steps          int64      `json:"steps"`
	ConsumedTokens int64      `json:"consumed_tokens"`
	Artifacts      []Artifact `json:"artifacts"`
}

type Backend interface {
	Run(context.Context, Request) (Observation, error)
}
