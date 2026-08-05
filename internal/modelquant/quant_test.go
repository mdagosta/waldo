package modelquant

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProfile(t *testing.T) {
	for requested, expected := range map[string]string{"2": "Q2_K", "4": "Q4_K_M", "8": "Q8_0"} {
		actual, err := ResolveProfile(requested)
		if err != nil || actual != expected {
			t.Fatalf("ResolveProfile(%q) = %q, %v", requested, actual, err)
		}
	}
	if _, err := ResolveProfile("7"); err == nil {
		t.Fatal("unsupported quantization was accepted")
	}
}

func TestRuntimeQuantizesWithCalibration(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "input.gguf")
	output := filepath.Join(directory, "output.gguf")
	calibration := filepath.Join(directory, "calibration.txt")
	if err := os.WriteFile(input, []byte("high precision"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(calibration, []byte("sample"), 0o600); err != nil {
		t.Fatal(err)
	}
	quantizerPath := filepath.Join(directory, "llama-quantize")
	calibratorPath := filepath.Join(directory, "llama-imatrix")
	writeExecutable(t, calibratorPath, "#!/bin/sh\nout=\"\"\nwhile [ $# -gt 0 ]; do if [ \"$1\" = \"-o\" ]; then out=$2; shift 2; else shift; fi; done\nprintf matrix > \"$out\"\n")
	writeExecutable(t, quantizerPath, "#!/bin/sh\nif [ \"$1\" = \"--imatrix\" ]; then test -f \"$2\" || exit 9; shift 2; fi\ncp \"$1\" \"$2\"\n")
	calibratorTool := Tool{Name: "llama-imatrix", Version: "test", SHA256: "calibrator", path: calibratorPath}
	runtime := Runtime{Quantizer: Tool{Name: "llama-quantize", Version: "test", SHA256: "quantizer", path: quantizerPath}, Calibrator: &calibratorTool}
	result, err := runtime.Quantize(context.Background(), Request{Input: input, Output: output, Resolved: "Q4_K_M", CalibrationText: calibration})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "high precision" || result.Quantizer.Name != "llama-quantize" || result.Calibrator == nil {
		t.Fatalf("output/result = %q %+v", data, result)
	}
	if _, err := os.Stat(filepath.Join(directory, ".waldo-imatrix.gguf")); !os.IsNotExist(err) {
		t.Fatal("temporary importance matrix was not removed")
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}
