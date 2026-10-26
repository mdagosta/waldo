// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package join

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func huggingFaceEndpoint() string {
	if endpoint := strings.TrimRight(os.Getenv("HF_ENDPOINT"), "/"); endpoint != "" {
		return endpoint
	}
	return "https://huggingface.co"
}

func huggingFaceToken() string {
	if token := os.Getenv("HF_TOKEN"); token != "" {
		return token
	}
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".cache", "huggingface", "token")); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}


// uploadHuggingFace pushes directory to repository with the `hf` CLI and returns the new commit.
func uploadHuggingFace(ctx context.Context, repository, directory, message string, log io.Writer) (string, error) {
	command := exec.CommandContext(ctx, "hf", "upload", repository, directory, ".", "--commit-message", message)
	command.Stdout, command.Stderr = log, log
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("hf upload %s (needs the huggingface_hub CLI and a write token): %w", repository, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, huggingFaceEndpoint()+"/api/models/"+repository+"/revision/main", nil)
	if err != nil {
		return "", err
	}
	if token := huggingFaceToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var revision struct {
		SHA string `json:"sha"`
	}
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &revision) != nil || revision.SHA == "" {
		return "", fmt.Errorf("resolve %s after upload: %s: %s", repository, response.Status, bytes.TrimSpace(body))
	}
	return revision.SHA, nil
}
