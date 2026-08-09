// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/corpus"
)

func TestParseModelTrainDefaultsAndValidatesEpochs(t *testing.T) {
	context, args, err := parseCobraCommand(t, []string{"model", "train"}, []string{"foo", "core/books", "science/papers"})
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != "foo" || int64Option(context, "epochs") != 1 || len(args[1:]) != 2 {
		t.Fatalf("args = %v, epochs = %d", args, int64Option(context, "epochs"))
	}
	context, _, err = parseCobraCommand(t, []string{"model", "train"}, []string{"foo", "core/books", "--epochs", "3"})
	if err != nil || int64Option(context, "epochs") != 3 {
		t.Fatalf("epochs = %d, error = %v", int64Option(context, "epochs"), err)
	}
	context, _, err = parseCobraCommand(t, []string{"model", "train"}, []string{"foo", "core/books", "--audit"})
	if err != nil || !boolOption(context, "audit") {
		t.Fatalf("audit = %v, error = %v", boolOption(context, "audit"), err)
	}
	context, _, err = parseCobraCommand(t, []string{"model", "compose"}, []string{"foo", "compose.yaml"})
	if err != nil || boolOption(context, "audit") {
		t.Fatalf("compose audit default = %v, error = %v", boolOption(context, "audit"), err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"model", "train", "foo", "core/books", "--epochs", "0"}, &stdout, &stderr); code != 1 {
		t.Fatal("zero epochs accepted")
	}
}

func TestModelMaterializeProgressReportsEveryCompletedShardToLogs(t *testing.T) {
	var output bytes.Buffer
	report := modelMaterializeProgressPrinter(&output)
	for position := 1; position <= 3; position++ {
		report(corpus.MaterializeProgress{
			Phase: "complete", Current: position, Total: 3,
			Bytes: int64(position * 10), TotalBytes: 30,
			Shard: corpus.ShardPin{SHA256: strings.Repeat(string(rune('a'+position-1)), 64)},
		})
	}
	if strings.Count(output.String(), "materialized") != 3 || !strings.Contains(output.String(), "30 B/30 B") {
		t.Fatalf("progress output = %q", output.String())
	}
}

func TestParseModelTrainDiagnosesOmittedModelName(t *testing.T) {
	for _, path := range []string{"core/common-pile/python-enhancement-proposals/peps", "."} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"model", "train", path}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "model name is required") {
			t.Fatalf("path %q: code = %d, error = %q", path, code, stderr.String())
		}
	}
}

func TestModelTrainFormatsOmittedNameDiagnostic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := "core/common-pile/python-enhancement-proposals/peps"
	if code := Run([]string{"model", "train", path}, &stdout, &stderr); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	wantError := "Error: model name is required before index path \"" + path + "\"\n"
	if stderr.String() != wantError {
		t.Fatalf("stderr = %q, want %q", stderr.String(), wantError)
	}
	if !strings.Contains(stdout.String(), "Usage:\n  waldo model train <name> [index-path...] [flags]") {
		t.Fatalf("stdout does not contain Cobra usage: %q", stdout.String())
	}
}
