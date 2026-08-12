// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/config"
)

// TestModelTrainWorkerRejectsInvalidTopology covers the secondary-node flag
// validation, which fails closed before any rendezvous is attempted.
func TestModelTrainWorkerRejectsInvalidTopology(t *testing.T) {
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errs bytes.Buffer
	if code := Run([]string{"config", "set", "model.backend", "torchtitan"}, &out, &errs); code != 0 {
		t.Fatalf("seed config: %s", errs.String())
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"single node", []string{"model", "train-worker", "--nodes", "1", "--node-rank", "1", "--rendezvous", "h:1", "--rendezvous-id", "r"}, "greater than 1"},
		{"rank zero", []string{"model", "train-worker", "--nodes", "4", "--node-rank", "0", "--rendezvous", "h:1", "--rendezvous-id", "r"}, "node-rank must be in 1..3"},
		{"rank too high", []string{"model", "train-worker", "--nodes", "4", "--node-rank", "4", "--rendezvous", "h:1", "--rendezvous-id", "r"}, "node-rank must be in 1..3"},
		{"missing rendezvous", []string{"model", "train-worker", "--nodes", "4", "--node-rank", "1", "--rendezvous-id", "r"}, "requires --rendezvous"},
		{"malformed rendezvous", []string{"model", "train-worker", "--nodes", "4", "--node-rank", "1", "--rendezvous", "hostonly", "--rendezvous-id", "r"}, "must be host:port"},
		{"traversal rendezvous id", []string{"model", "train-worker", "--nodes", "4", "--node-rank", "1", "--rendezvous", "h:1", "--rendezvous-id", ".."}, "must start with a letter or digit"},
		{"separator rendezvous id", []string{"model", "train-worker", "--nodes", "4", "--node-rank", "1", "--rendezvous", "h:1", "--rendezvous-id", "a/b"}, "must start with a letter or digit"},
		{"zero nodes", []string{"model", "train-worker", "--nodes", "0", "--node-rank", "1", "--rendezvous", "h:1", "--rendezvous-id", "r"}, "--nodes must be an integer greater than or equal to 1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(test.args, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected failure, got success; stdout = %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), test.want)
			}
		})
	}
}

// TestConfigNCCLKeysRoundTrip covers the machine-local NCCL configuration keys.
func TestConfigNCCLKeysRoundTrip(t *testing.T) {
	t.Setenv("WALDO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	run := func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}
	if _, stderr, code := run("config", "set", "model.nccl.interface", "roce0"); code != 0 {
		t.Fatalf("set iface: %s", stderr)
	}
	if _, stderr, code := run("config", "set", "model.nccl.hca", "mlx5_0"); code != 0 {
		t.Fatalf("set hca: %s", stderr)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.NCCLInterface != "roce0" || loaded.Model.NCCLHCA != "mlx5_0" {
		t.Fatalf("config = %+v", loaded.Model)
	}
	if out, _, code := run("config", "get", "model.nccl.interface"); code != 0 || !strings.Contains(out, "roce0") {
		t.Fatalf("get iface out = %q code = %d", out, code)
	}
	if _, stderr, code := run("config", "unset", "model.nccl.interface"); code != 0 {
		t.Fatalf("unset iface: %s", stderr)
	}
	loaded, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model.NCCLInterface != "" {
		t.Fatalf("iface not cleared: %+v", loaded.Model)
	}
}
