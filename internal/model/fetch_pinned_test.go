// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchPinnedFetchesOnlyThePinnedRelease(t *testing.T) {
	release := t.TempDir()
	files := map[string]string{}
	for name, content := range map[string]string{"model.safetensors": "weights", "BOM.json": "{}"} {
		if err := os.WriteFile(filepath.Join(release, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(content))
		files[name] = hex.EncodeToString(digest[:])
	}
	target := filepath.Join(t.TempDir(), "fetched")
	if err := (Puller{}).FetchPinned(context.Background(), "file://"+release, files, target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(target, "model.safetensors")); err != nil || string(data) != "weights" {
		t.Fatalf("fetched %q, %v", data, err)
	}
	// A file that changed since it was pinned refuses the whole release.
	tampered := map[string]string{"model.safetensors": files["model.safetensors"], "BOM.json": strings.Repeat("0", 64)}
	other := filepath.Join(t.TempDir(), "other")
	if err := (Puller{}).FetchPinned(context.Background(), "file://"+release, tampered, other); err == nil || !strings.Contains(err.Error(), "pinned SHA-256") {
		t.Fatalf("tampered error = %v", err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("a refused release left files behind")
	}
	// From Hugging Face, only at a pinned commit.
	commit := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := filepath.Base(request.URL.Path)
		if !strings.Contains(request.URL.Path, "/someone/model/resolve/"+commit+"/") {
			http.NotFound(writer, request)
			return
		}
		data, _ := os.ReadFile(filepath.Join(release, name))
		_, _ = writer.Write(data)
	}))
	defer server.Close()
	puller := Puller{Endpoint: server.URL, Token: "unused"}
	hub := filepath.Join(t.TempDir(), "hub")
	if err := puller.FetchPinned(context.Background(), "huggingface://someone/model@"+commit, files, hub); err != nil {
		t.Fatal(err)
	}
	if err := puller.FetchPinned(context.Background(), "huggingface://someone/model@main", files, hub); err == nil || !strings.Contains(err.Error(), "pin a commit") {
		t.Fatalf("unpinned error = %v", err)
	}
}
