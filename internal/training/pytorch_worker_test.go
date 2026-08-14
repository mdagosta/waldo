// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"regexp"
	"testing"
)

// The artifact check in ADR 0044 exists to detect serialization, precision, and
// loader faults in the persisted weights. It can only measure those if the
// reloaded model is evaluated exactly as the live model was. Evaluating the two
// at different compute precisions instead measures autocast rounding, which
// accumulates with depth until it exceeds the tolerance for every sufficiently
// large model while revealing nothing about the artifact.
func TestPyTorchWorkerEvaluatesArtifactAtLiveEvaluationPrecision(t *testing.T) {
	pattern := regexp.MustCompile(`evaluate_model\([^)]*mixed_precision=(True|False)\)`)
	matches := pattern.FindAllStringSubmatch(string(pyTorchWorker), -1)
	if len(matches) < 2 {
		t.Fatalf("expected the worker to evaluate both the live and reloaded model, found %d call(s)", len(matches))
	}
	for _, match := range matches[1:] {
		if match[1] != matches[0][1] {
			t.Fatalf("held-out evaluations disagree on compute precision: %q vs %q", matches[0][0], match[0])
		}
	}
}
