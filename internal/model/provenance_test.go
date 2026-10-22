// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
)

// withClosure adds a release training closure over the named corpus hashes to a package.
func withClosure(t *testing.T, directory string, corpora ...string) {
	t.Helper()
	closure := emptyClosure(true)
	for _, digest := range corpora {
		closure.Corpora = append(closure.Corpora, ClosureCorpus{CorpusBOMSHA256: digest, Uses: 1, CorpusBOM: corpus.BOM{Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{"code/" + digest[:4]}}})
	}
	data, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ClosureFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTrainingClosureFollowsTheWholeLineage(t *testing.T) {
	common, first, second := strings.Repeat("a", 64), strings.Repeat("1", 64), strings.Repeat("2", 64)
	left, right, bare := releasePackage(t, common, 0), releasePackage(t, common, 1), releasePackage(t, common, 2)
	withClosure(t, left, first)
	withClosure(t, right, first, second)
	root := t.TempDir()
	for name, directory := range map[string]string{"left": left, "right": right, "bare": bare} {
		if _, err := (Puller{Root: root}).PullDirectory(name, directory); err != nil {
			t.Fatal(err)
		}
	}
	merged, err := (Builder{Root: root}).Compose(context.Background(), "merged", Compose{Kind: "waldo-model-compose", Schema: ComposeSchema, Base: &ComposeBase{Merge: []ComposeMergeParent{{Model: "left"}, {Model: "right"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := ModelClosure(merged)
	if err != nil {
		t.Fatal(err)
	}
	// Both parents trained on the first corpus, so the merge counts it twice and lists it once.
	if !closure.Complete || len(closure.Corpora) != 2 || closure.Corpora[0].CorpusBOMSHA256 != first || closure.Corpora[0].Uses != 2 || closure.Corpora[1].Uses != 1 {
		t.Fatalf("merged closure = %+v", closure)
	}
	pulled, err := Inspect(root, "bare")
	if err != nil {
		t.Fatal(err)
	}
	// A release that recorded no closure leaves its history unrecorded, not empty.
	if inherited, err := InheritedClosure(pulled); err != nil || inherited.Complete {
		t.Fatalf("closure of a release without one = %+v, %v", inherited, err)
	}
	if fresh := emptyClosure(true); !unionClosures(fresh).Complete {
		t.Fatal("a model trained from scratch has a complete, empty history")
	}
}

func TestPullRefusesAMalformedTrainingClosure(t *testing.T) {
	directory := releasePackage(t, strings.Repeat("a", 64), 0)
	if err := os.WriteFile(filepath.Join(directory, ClosureFile), []byte(`{"kind":"something-else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Puller{Root: t.TempDir()}).PullDirectory("bad", directory); err == nil || !strings.Contains(err.Error(), "training closure") {
		t.Fatalf("malformed closure error = %v", err)
	}
}
