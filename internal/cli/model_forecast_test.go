// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/model"
)

func TestApproximateDurationUsesHoursUntilOneHundred(t *testing.T) {
	for _, test := range []struct {
		seconds int64
		want    string
	}{
		{seconds: 30 * 60, want: "under 1 hour"},
		{seconds: 60 * 60, want: "1 hour"},
		{seconds: 99 * 60 * 60, want: "99 hours"},
		{seconds: 100 * 60 * 60, want: "4 days"},
		{seconds: 12 * 24 * 60 * 60, want: "12 days"},
	} {
		if got := approximateDuration(test.seconds); got != test.want {
			t.Errorf("approximateDuration(%d) = %q, want %q", test.seconds, got, test.want)
		}
	}
}

func TestModelForecastAcceptsConfiguredMultipleIndexPaths(t *testing.T) {
	root := fixtureCLIIndex(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WALDO_CONFIG", configPath)
	t.Chdir(t.TempDir())
	if err := config.Save(config.Config{
		Index: root,
		Lookaside: config.Lookaside{
			Cache: filepath.Join(t.TempDir(), "cache"), Scratch: filepath.Join(t.TempDir(), "scratch"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--json", "model", "forecast", "./books", "books/books.json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var output struct {
		Paths      []string `json:"paths"`
		Preset     string   `json:"preset"`
		Tokens     int64    `json:"tokens"`
		Parameters uint64   `json:"approximate_parameters"`
		Forecast   struct {
			Configurations []model.HardwareConfiguration `json:"configurations"`
		} `json:"forecast"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Tokens != 2 || output.Preset != "10m" || output.Parameters == 0 || len(output.Paths) != 2 || len(output.Forecast.Configurations) == 0 {
		t.Fatalf("output = %+v", output)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--json", "model", "forecast"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("whole-index forecast code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "warning: no index path specified; using the entire configured index "+root) {
		t.Fatalf("whole-index forecast stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"model", "forecast", filepath.Join(root, "books")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("directory forecast code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"MODEL:", "10m", "TOKENS:", "BUDGET:", "one pass", "MFR", "ACCELERATOR"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("forecast missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestWriteModelForecastUsesApprovedCompactColumns(t *testing.T) {
	report := model.ResourceForecast{Configurations: []model.HardwareConfiguration{
		{Manufacturer: "Apple", Accelerator: "M4 Max 40-core GPU", GPUs: 1, MemoryPerGPUBytes: 128 << 30, ApproximateSeconds: 48 * 24 * 60 * 60},
		{Manufacturer: "NVIDIA", Accelerator: "H100 SXM", GPUs: 8, MemoryPerGPUBytes: 80 << 30, ApproximateSeconds: 44 * 60 * 60},
	}}
	var output bytes.Buffer
	writeModelForecast(&output, report)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("output = %q", output.String())
	}
	for lineNumber, want := range []string{"GPUS", "1", "8"} {
		fields := strings.Fields(lines[lineNumber])
		if len(fields) == 0 || fields[0] != want {
			t.Errorf("line %d does not lead with %q:\n%s", lineNumber+1, want, lines[lineNumber])
		}
	}
	for _, want := range []string{"MFR", "ACCELERATOR", "GPUS", "MEMORY/GPU", "APPROX. TIME", "Apple", "128 GB", "48 days", "NVIDIA", "80 GB", "44 hours"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	for _, unwanted := range []string{"BACKEND", "FIT", "~", "unified"} {
		if strings.Contains(output.String(), unwanted) {
			t.Errorf("output unexpectedly contains %q:\n%s", unwanted, output.String())
		}
	}
}
