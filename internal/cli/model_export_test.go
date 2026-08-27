// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestParseModelExportFormats(t *testing.T) {
	for _, format := range []string{"waldo", "huggingface", "mlx", "gguf", "ollama"} {
		context, args, err := parseCobraCommand(t, []string{"model", "export"}, []string{"model", "release", "--format", format, "--allow-incomplete"})
		if err == nil {
			var parsed modelExportOptions
			parsed, err = cobraModelExportOptions(context, args)
			if err == nil && (parsed.Name != "model" || parsed.Destination != "release" || parsed.Format != format || !parsed.AllowIncomplete) {
				t.Fatalf("format %s parsed as %+v", format, parsed)
			}
		}
		if err != nil {
			t.Fatalf("format %s: %v", format, err)
		}
	}
	context, args, err := parseCobraCommand(t, []string{"model", "export"}, []string{"model", "release", "--format", "made-up"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cobraModelExportOptions(context, args); err == nil {
		t.Fatal("unknown format was accepted")
	}
}

func TestParseModelExportQuantization(t *testing.T) {
	context, args, err := parseCobraCommand(t, []string{"model", "export"}, []string{"model", "release", "--format=gguf", "--quant", "4", "--calibration", "core/books"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := cobraModelExportOptions(context, args)
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
		context, args, err := parseCobraCommand(t, []string{"model", "export"}, arguments)
		if err == nil {
			_, err = cobraModelExportOptions(context, args)
		}
		if err == nil {
			t.Fatalf("accepted invalid arguments %v", arguments)
		}
	}
}

func TestParseModelExportOllamaTools(t *testing.T) {
	context, args, err := parseCobraCommand(t, []string{"model", "export"}, []string{"model", "release", "--format", "ollama", "--ollama-tools"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := cobraModelExportOptions(context, args)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.OllamaTools {
		t.Fatalf("parsed = %+v", parsed)
	}
	context, args, err = parseCobraCommand(t, []string{"model", "export"}, []string{"model", "release", "--format", "gguf", "--ollama-tools"})
	if err == nil {
		_, err = cobraModelExportOptions(context, args)
	}
	if err == nil {
		t.Fatal("accepted --ollama-tools for a non-Ollama export")
	}
}
