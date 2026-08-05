package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo-new/internal/training"
)

func validateBackendObservation(runDirectory string, planned PlannedStage, observation training.Observation) error {
	if observation.Steps < 0 || observation.Steps > planned.Parameters.Steps {
		return fmt.Errorf("reported steps %d are outside planned range 0..%d", observation.Steps, planned.Parameters.Steps)
	}
	if observation.ConsumedTokens < 0 || observation.ConsumedTokens > planned.PlannedTokens {
		return fmt.Errorf("reported tokens %d are outside planned range 0..%d", observation.ConsumedTokens, planned.PlannedTokens)
	}
	if len(observation.Artifacts) == 0 {
		return fmt.Errorf("reported no output artifacts")
	}
	seen := make(map[string]bool, len(observation.Artifacts))
	for _, artifact := range observation.Artifacts {
		if artifact.Path == "" || filepath.IsAbs(filepath.FromSlash(artifact.Path)) {
			return fmt.Errorf("artifact path %q must be relative", artifact.Path)
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(artifact.Path)))
		if clean != artifact.Path || clean == "artifacts" || !strings.HasPrefix(clean, "artifacts/") {
			return fmt.Errorf("artifact path %q must be canonical beneath artifacts/", artifact.Path)
		}
		if seen[clean] {
			return fmt.Errorf("artifact path %q is duplicated", artifact.Path)
		}
		seen[clean] = true
		path := filepath.Join(runDirectory, filepath.FromSlash(clean))
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("artifact %s: %w", artifact.Path, err)
		}
		hasher := sha256.New()
		bytes, copyErr := io.Copy(hasher, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("artifact %s: %w", artifact.Path, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("artifact %s: %w", artifact.Path, closeErr)
		}
		if bytes != artifact.Bytes {
			return fmt.Errorf("artifact %s size is %d, backend reported %d", artifact.Path, bytes, artifact.Bytes)
		}
		digest := hex.EncodeToString(hasher.Sum(nil))
		if digest != artifact.SHA256 {
			return fmt.Errorf("artifact %s SHA-256 is %s, backend reported %s", artifact.Path, digest, artifact.SHA256)
		}
	}
	return nil
}
