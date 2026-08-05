package cli

import "testing"

func TestParseModelExportFormats(t *testing.T) {
	for _, format := range []string{"waldo", "huggingface"} {
		name, destination, got, incomplete, err := parseModelExport([]string{"model", "release", "--format", format, "--allow-incomplete"})
		if err != nil {
			t.Fatalf("format %s: %v", format, err)
		}
		if name != "model" || destination != "release" || got != format || !incomplete {
			t.Fatalf("format %s parsed as %q %q %q %t", format, name, destination, got, incomplete)
		}
	}
	if _, _, _, _, err := parseModelExport([]string{"model", "release", "--format", "gguf"}); err == nil {
		t.Fatal("unimplemented GGUF format was accepted")
	}
}
