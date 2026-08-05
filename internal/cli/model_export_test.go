package cli

import "testing"

func TestParseModelExportFormats(t *testing.T) {
	for _, format := range []string{"waldo", "huggingface", "mlx", "gguf", "ollama"} {
		parsed, err := parseModelExport([]string{"model", "release", "--format", format, "--allow-incomplete"})
		if err != nil {
			t.Fatalf("format %s: %v", format, err)
		}
		if parsed.Name != "model" || parsed.Destination != "release" || parsed.Format != format || !parsed.AllowIncomplete {
			t.Fatalf("format %s parsed as %+v", format, parsed)
		}
	}
	if _, err := parseModelExport([]string{"model", "release", "--format", "made-up"}); err == nil {
		t.Fatal("unknown format was accepted")
	}
}

func TestParseModelExportQuantization(t *testing.T) {
	parsed, err := parseModelExport([]string{"model", "release", "--format=gguf", "--quant", "4", "--calibration", "core/books"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Quant != "4" || parsed.Calibration != "core/books" {
		t.Fatalf("parsed = %+v", parsed)
	}
	for _, arguments := range [][]string{
		{"model", "release", "--format", "mlx", "--quant", "4"},
		{"model", "release", "--format", "gguf", "--calibration", "core/books"},
		{"model", "release", "--format", "gguf", "--quant", "7"},
	} {
		if _, err := parseModelExport(arguments); err == nil {
			t.Fatalf("accepted invalid arguments %v", arguments)
		}
	}
}
