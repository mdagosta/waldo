// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

func TestParseModelTrainDefaultsAndValidatesEpochs(t *testing.T) {
	name, paths, epochs, err := parseModelTrain([]string{"foo", "core/books", "science/papers"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "foo" || epochs != 1 || len(paths) != 2 {
		t.Fatalf("name = %q, paths = %v, epochs = %d", name, paths, epochs)
	}
	_, _, epochs, err = parseModelTrain([]string{"foo", "core/books", "--epochs", "3"})
	if err != nil || epochs != 3 {
		t.Fatalf("epochs = %d, error = %v", epochs, err)
	}
	if _, _, _, err := parseModelTrain([]string{"foo", "core/books", "--epochs", "0"}); err == nil {
		t.Fatal("zero epochs accepted")
	}
}

func TestParseModelTrainDiagnosesOmittedModelName(t *testing.T) {
	_, _, _, err := parseModelTrain([]string{"core/common-pile/python-enhancement-proposals/peps"})
	if err == nil || !strings.Contains(err.Error(), "model name is required before index path") {
		t.Fatalf("error = %v", err)
	}
	if _, _, _, err := parseModelTrain([]string{"."}); err == nil || !strings.Contains(err.Error(), "model name is required") {
		t.Fatalf("dot-path error = %v", err)
	}
}
