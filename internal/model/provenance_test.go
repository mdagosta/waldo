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

// withClosure adds a release training closure to a package: its own run BOMs' corpora and the named inherited ones.
func withClosure(t *testing.T, directory string, corpora ...string) {
	t.Helper()
	closure := emptyClosure(true)
	for _, digest := range corpora {
		closure.Corpora = append(closure.Corpora, ClosureCorpus{CorpusBOMSHA256: digest, Uses: 1, CorpusBOM: corpus.BOM{Kind: "openwaldo-bom", Schema: 1, Subject: "corpus", Paths: []string{"code/" + digest[:4]}}})
	}
	runs, err := filepath.Glob(filepath.Join(directory, "provenance-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range runs {
		var run RunBOM
		data, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(data, &run) != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		closure.Corpora = append(closure.Corpora, ClosureCorpus{CorpusBOMSHA256: run.CorpusBOMSHA256, Uses: 1, CorpusBOM: run.CorpusBOM})
	}
	closure = unionClosures(closure)
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
	// Both parents trained on the first corpus, so the merge counts it twice and lists it once,
	// beside the second corpus and each parent's own run.
	uses := map[string]int{}
	for _, entry := range closure.Corpora {
		uses[entry.CorpusBOMSHA256] = entry.Uses
	}
	if !closure.Complete || len(closure.Corpora) != 4 || uses[first] != 2 || uses[second] != 1 {
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

func TestReleaseClosuresMustCoverTheirRuns(t *testing.T) {
	// A closure that leaves out the release's own run is refused on pull.
	directory := releasePackage(t, strings.Repeat("a", 64), 0)
	closure := emptyClosure(true)
	closure.Corpora = append(closure.Corpora, ClosureCorpus{CorpusBOMSHA256: strings.Repeat("1", 64), Uses: 1})
	data, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ClosureFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Puller{Root: t.TempDir()}).PullDirectory("omits", directory); err == nil || !strings.Contains(err.Error(), "leaves out corpus") {
		t.Fatalf("omitting closure error = %v", err)
	}
	// A closure that covers its run but invents an inherited corpus fails CheckClosure against its true start.
	root := t.TempDir()
	honest, padded := releasePackage(t, strings.Repeat("a", 64), 1), releasePackage(t, strings.Repeat("a", 64), 2)
	withClosure(t, honest)
	withClosure(t, padded, strings.Repeat("9", 64))
	for name, directory := range map[string]string{"honest": honest, "padded": padded} {
		if _, err := (Puller{Root: root}).PullDirectory(name, directory); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]bool{"honest": true, "padded": false} {
		inspection, err := Inspect(root, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckClosure(inspection, nil); (err == nil) != want {
			t.Fatalf("%s: CheckClosure = %v", name, err)
		}
	}
}

func TestPullKeepsTheRoundRecord(t *testing.T) {
	for name, content := range map[string]string{
		"kept":      `{"kind":"openwaldo-bom","schema":1,"subject":"join-round","run":"r","round":1}`,
		"malformed": `{"kind":"something-else"}`,
	} {
		directory := releasePackage(t, strings.Repeat("a", 64), 0)
		withClosure(t, directory)
		if err := os.WriteFile(filepath.Join(directory, RoundFile), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		pulled, err := (Puller{Root: t.TempDir()}).PullDirectory(name, directory)
		if name == "malformed" {
			if err == nil || !strings.Contains(err.Error(), RoundFile) {
				t.Fatalf("malformed round record error = %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		kept := false
		for _, artifact := range pulled.Origin.Artifacts {
			kept = kept || artifact.Role == RoundRole
		}
		if !kept {
			t.Fatalf("round record not kept: %+v", pulled.Origin.Artifacts)
		}
	}
}
