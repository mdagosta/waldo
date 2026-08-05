// Package mlxruntime owns the Python model definition shared by WALDO's MLX
// training and inference workers.
package mlxruntime

import _ "embed"

//go:embed model.py
var modelSource string

func WithModel(worker []byte) string {
	return modelSource + "\n" + string(worker)
}
