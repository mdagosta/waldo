package model

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/training"
)

func TestValidateBackendObservationVerifiesBoundsAndArtifacts(t *testing.T) {
	runDirectory := t.TempDir()
	artifactPath := filepath.Join(runDirectory, "artifacts", "weights.safetensors")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("weights")
	if err := os.WriteFile(artifactPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	planned := PlannedStage{PlannedTokens: 128, Parameters: training.Parameters{Steps: 2}}
	valid := training.Observation{
		Steps: 2, ConsumedTokens: 128,
		Artifacts: []training.Artifact{{Path: "artifacts/weights.safetensors", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))}},
	}
	if err := validateBackendObservation(runDirectory, planned, valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*training.Observation)
		want   string
	}{
		{name: "steps", mutate: func(value *training.Observation) { value.Steps = 3 }, want: "reported steps"},
		{name: "tokens", mutate: func(value *training.Observation) { value.ConsumedTokens = 129 }, want: "reported tokens"},
		{name: "path", mutate: func(value *training.Observation) { value.Artifacts[0].Path = "../weights" }, want: "canonical beneath"},
		{name: "size", mutate: func(value *training.Observation) { value.Artifacts[0].Bytes++ }, want: "size is"},
		{name: "hash", mutate: func(value *training.Observation) { value.Artifacts[0].SHA256 = strings.Repeat("0", 64) }, want: "SHA-256"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			candidate.Artifacts = append([]training.Artifact(nil), valid.Artifacts...)
			test.mutate(&candidate)
			if err := validateBackendObservation(runDirectory, planned, candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
