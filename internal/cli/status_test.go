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
)

func TestStatusExplainsEveryUnavailableReadiness(t *testing.T) {
	root := fixtureCLIIndex(t)
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := config.Save(config.Config{Index: root, Model: config.Model{Backend: "fake"}}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"status"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status code = %d, stderr = %q", code, stderr.String())
	}
	for _, heading := range []string{"Host\n", "Index\n", "Lookaside\n", "Training\n", "Overall\n"} {
		if !strings.Contains(stdout.String(), heading) {
			t.Errorf("status output missing section %q:\n%s", heading, stdout.String())
		}
	}
	for _, want := range []string{"Host", "Memory", "Index", root, "Lookaside", "Training", "Ready", "no", "Reason", "fake backend"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("status output missing %q:\n%s", want, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--json", "status"}, &stdout, &stderr); code != 0 {
		t.Fatalf("JSON status code = %d, stderr = %q", code, stderr.String())
	}
	var output struct {
		Ready bool `json:"ready"`
		Index struct {
			Ready bool `json:"ready"`
		} `json:"index"`
		Training struct {
			Ready  bool   `json:"ready"`
			Reason string `json:"reason"`
		} `json:"training"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Ready || !output.Index.Ready || output.Training.Ready || output.Training.Reason == "" {
		t.Fatalf("status = %+v", output)
	}
}
