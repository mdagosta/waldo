package training

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/openwaldo/waldo-new/internal/corpus"
)

const FakeRevision = "builtin-fake-schema-1"

// Fake proves orchestration and provenance. Its artifact explicitly states
// that it is not trained weights and must never be accepted by a real backend.
type Fake struct{}

func (Fake) Run(ctx context.Context, request Request) (Observation, error) {
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	payload, err := json.MarshalIndent(struct {
		Kind         string     `json:"kind"`
		Schema       int        `json:"schema"`
		Warning      string     `json:"warning"`
		Architecture string     `json:"architecture_sha256"`
		BOM          corpus.BOM `json:"corpus_bom"`
		Parameters   Parameters `json:"parameters"`
	}{
		Kind: "waldo-fake-artifact", Schema: 1,
		Warning:      "simulation only; this file contains no trained model weights",
		Architecture: request.ArchitectureSHA256, BOM: request.BOM, Parameters: request.Parameters,
	}, "", "  ")
	if err != nil {
		return Observation{}, err
	}
	payload = append(payload, '\n')
	if err := writeArtifactAtomic(request.OutputPath, payload); err != nil {
		return Observation{}, fmt.Errorf("write fake artifact: %w", err)
	}
	digest := sha256.Sum256(payload)
	capacity := int64(0)
	if request.Parameters.Steps > 0 && request.Parameters.BatchSize > 0 && request.Parameters.SequenceLength > 0 && request.Parameters.Steps <= math.MaxInt64/request.Parameters.BatchSize {
		capacity = request.Parameters.Steps * request.Parameters.BatchSize
		if capacity <= math.MaxInt64/request.Parameters.SequenceLength {
			capacity *= request.Parameters.SequenceLength
		} else {
			capacity = request.BOM.Totals.Tokens
		}
	}
	if capacity > request.BOM.Totals.Tokens {
		capacity = request.BOM.Totals.Tokens
	}
	return Observation{
		Simulated: true, Steps: request.Parameters.Steps, ConsumedTokens: capacity,
		Artifacts: []Artifact{{Path: request.ArtifactPath, SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(payload))}},
	}, nil
}

func writeArtifactAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".waldo-artifact-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}
