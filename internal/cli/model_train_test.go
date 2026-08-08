// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"
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
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"model", "train", "foo", "core/books", "--epochs", "0"}, &stdout, &stderr); code != 2 {
		t.Fatal("zero epochs accepted")
	}
}

func TestParseModelTrainDiagnosesOmittedModelName(t *testing.T) {
	for _, path := range []string{"core/common-pile/python-enhancement-proposals/peps", "."} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"model", "train", path}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "model name is required") {
			t.Fatalf("path %q: code = %d, error = %q", path, code, stderr.String())
		}
	}
}

func TestModelTrainFormatsOmittedNameDiagnostic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := "core/common-pile/python-enhancement-proposals/peps"
	if code := Run([]string{"model", "train", path}, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	want := "waldo: model name is required before index path \"" + path + "\"\n\nUsage:\n  waldo model train <name> [index-path...] [--epochs <n>] [--json]\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}
