package inference

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo-new/internal/model"
	"github.com/openwaldo/waldo-new/internal/training"
)

type Artifacts struct {
	Model         string
	RunID         string
	Backend       training.Identity
	ContextTokens int
	Weights       string
	Configuration string
	Tokenizer     string
}

func ResolveArtifacts(inspection model.Inspection) (Artifacts, error) {
	if inspection.BOM.CurrentRunID == "" {
		return Artifacts{}, fmt.Errorf("model %q has no complete non-simulated run with usable weights", inspection.Model.Name)
	}
	var selected *model.ModelBOMRun
	for index := range inspection.BOM.Runs {
		if inspection.BOM.Runs[index].ID == inspection.BOM.CurrentRunID {
			selected = &inspection.BOM.Runs[index]
			break
		}
	}
	if selected == nil || selected.State != model.RunComplete || selected.Simulated {
		return Artifacts{}, fmt.Errorf("model %q current run %q is not a complete real run", inspection.Model.Name, inspection.BOM.CurrentRunID)
	}
	result := Artifacts{
		Model: inspection.Model.Name, RunID: selected.ID, Backend: selected.Backend,
		ContextTokens: int(inspection.Model.Architecture.ContextTokens),
	}
	for _, artifact := range selected.Artifacts {
		path, err := resolveModelPath(inspection.Path, artifact.Path)
		if err != nil {
			return Artifacts{}, err
		}
		if err := verifyArtifact(path, artifact); err != nil {
			return Artifacts{}, err
		}
		switch artifact.Role {
		case "weights":
			result.Weights = path
		case "configuration":
			result.Configuration = path
		case "tokenizer":
			result.Tokenizer = path
		}
	}
	if result.Weights == "" || result.Configuration == "" || result.Tokenizer == "" {
		return Artifacts{}, fmt.Errorf("model %q current run %q must provide weights, configuration, and tokenizer artifacts", inspection.Model.Name, selected.ID)
	}
	return result, nil
}

func resolveModelPath(root, logical string) (string, error) {
	if logical == "" || filepath.IsAbs(filepath.FromSlash(logical)) {
		return "", fmt.Errorf("model artifact path %q is not model-root-relative", logical)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(logical)))
	if clean != logical || clean == "." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("model artifact path %q escapes the model root", logical)
	}
	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

func verifyArtifact(path string, artifact model.ModelBOMArtifact) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s artifact %s: %w", artifact.Role, artifact.Path, err)
	}
	hasher := sha256.New()
	bytes, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("hash %s artifact %s: %w", artifact.Role, artifact.Path, copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if bytes != artifact.Bytes {
		return fmt.Errorf("%s artifact %s has size %d, BOM requires %d", artifact.Role, artifact.Path, bytes, artifact.Bytes)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if digest != artifact.SHA256 {
		return fmt.Errorf("%s artifact %s has SHA-256 %s, BOM requires %s", artifact.Role, artifact.Path, digest, artifact.SHA256)
	}
	return nil
}
