// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package mlxruntime

import (
	"strings"
	"testing"
)

func TestSharedModelAppliesComposeDropout(t *testing.T) {
	for _, required := range []string{
		`architecture.get("dropout", 0.0)`,
		`self.residual_dropout = nn.Dropout(dropout)`,
		`self.residual_dropout(self.attention`,
		`self.residual_dropout(self.feed_forward`,
	} {
		if !strings.Contains(modelSource, required) {
			t.Fatalf("shared MLX model does not contain %q", required)
		}
	}
}
