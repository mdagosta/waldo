// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package mlxruntime owns the Python model definition shared by WALDO's MLX
// training and inference workers.
package mlxruntime

import _ "embed"

//go:embed model.py
var modelSource string

func WithModel(worker []byte) string {
	return modelSource + "\n" + string(worker)
}
