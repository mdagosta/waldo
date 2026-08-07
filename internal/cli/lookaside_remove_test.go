// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/lookaside"
)

func TestLookasideRemoveDeletesExplicitLocalObjects(t *testing.T) {
	root := t.TempDir()
	baseURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Lookaside: config.Lookaside{Publish: &config.Publish{URL: baseURL, Workers: 2}}}); err != nil {
		t.Fatal(err)
	}
	publisher, err := lookaside.NewFilePublisher(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	first := publishRemovalFixture(t, publisher, []byte("first parquet object"))
	second := publishRemovalFixture(t, publisher, []byte("second parquet object"))

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"lookaside", "rm", first, second}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed 2 object(s)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	for _, digest := range []string{first, second} {
		if _, err := os.Stat(filepath.Join(root, digest[:2], digest[2:4], digest)); !os.IsNotExist(err) {
			t.Fatalf("object %s still exists: %v", digest, err)
		}
	}
}

func TestLookasideRemovePreflightsEntireList(t *testing.T) {
	root := t.TempDir()
	baseURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String()
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Lookaside: config.Lookaside{Publish: &config.Publish{URL: baseURL, Workers: 2}}}); err != nil {
		t.Fatal(err)
	}
	publisher, err := lookaside.NewFilePublisher(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	existing := publishRemovalFixture(t, publisher, []byte("keep when another name is missing"))
	missingSum := sha256.Sum256([]byte("missing"))
	missing := hex.EncodeToString(missingSum[:])

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"lookaside", "rm", existing, missing}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "does not exist") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if present, err := publisher.Contains(context.Background(), existing); err != nil || !present {
		t.Fatalf("existing object was removed during failed preflight: present=%v err=%v", present, err)
	}
}

func TestLookasideRemoveRejectsNonObjectNames(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"lookaside", "rm", "s3://bucket/prefix"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "64 lowercase hexadecimal") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func publishRemovalFixture(t *testing.T, publisher *lookaside.FilePublisher, content []byte) string {
	t.Helper()
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	source := filepath.Join(t.TempDir(), digest)
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), source, digest, int64(len(content)), nil); err != nil {
		t.Fatal(err)
	}
	return digest
}
