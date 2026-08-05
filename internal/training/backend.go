// Package training is the narrow adapter boundary between WALDO's durable
// model lifecycle and an execution framework.
package training

import (
	"context"
	"encoding/json"

	"github.com/openwaldo/waldo-new/internal/corpus"
)

type Identity struct {
	Name     string `json:"name" yaml:"name"`
	Revision string `json:"revision" yaml:"revision"`
}

type Capabilities struct {
	Objectives       []string `json:"objectives"`
	CheckpointResume bool     `json:"checkpoint_resume"`
	Distributed      bool     `json:"distributed"`
	Safetensors      bool     `json:"safetensors"`
}

type Descriptor struct {
	Identity     Identity     `json:"identity"`
	Framework    string       `json:"framework"`
	Capabilities Capabilities `json:"capabilities"`
}

type Host struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type Accelerator struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	MemoryBytes  uint64 `json:"memory_bytes"`
}

// Execution is the immutable environment selected for a build. It is
// persisted by the model domain; adapters never write lifecycle records.
type Execution struct {
	Backend      Identity      `json:"backend"`
	Framework    string        `json:"framework"`
	Runtime      string        `json:"runtime"`
	Host         Host          `json:"host"`
	Accelerators []Accelerator `json:"accelerators,omitempty"`
	Nodes        int           `json:"nodes"`
	WorldSize    int           `json:"world_size"`
}

type ResolveRequest struct {
	ArchitectureSHA256 string
	Architecture       json.RawMessage
	Objectives         []string
}

type Selection struct {
	Backend   Backend
	Execution Execution
}

type Resolver interface {
	Resolve(context.Context, ResolveRequest) (Selection, error)
}

type ResolverFunc func(context.Context, ResolveRequest) (Selection, error)

func (function ResolverFunc) Resolve(ctx context.Context, request ResolveRequest) (Selection, error) {
	return function(ctx, request)
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
	Stage              string
	Objective          string
	ArchitectureSHA256 string
	Architecture       json.RawMessage
	BOM                corpus.BOM
	Inputs             []Input
	Parameters         Parameters
	ArtifactDirectory  string
	ArtifactPrefix     string
	Report             func(Event)
}

type Event struct {
	Message string  `json:"message"`
	Step    int64   `json:"step,omitempty"`
	Tokens  int64   `json:"tokens,omitempty"`
	Loss    float64 `json:"loss,omitempty"`
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
	Descriptor() Descriptor
	Run(context.Context, Request) (Observation, error)
}
