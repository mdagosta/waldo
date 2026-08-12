// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"math"
	"testing"
)

// TestForecastAddsNodeAwareDGXSpark covers Phase 3 of the multi-node work: the
// GB10 (DGX Spark) catalog row and the node dimension. A DGX Spark has one GPU
// per node, so a 2- or 4-GPU configuration is a 2- or 4-node cluster, and its
// effective throughput reflects the inter-node (RoCE) scaling factors. Every
// single-node accelerator must continue to report exactly one node.
func TestForecastAddsNodeAwareDGXSpark(t *testing.T) {
	if forecastCatalog != "openwaldo-training-hardware-2026-08-13" {
		t.Fatalf("forecast catalog id not bumped for the DGX Spark row: %s", forecastCatalog)
	}

	recipe := validCompose()
	architecture, err := recipe.Architecture.Forecast()
	if err != nil {
		t.Fatal(err)
	}
	report, err := forecastPlan(Plan{
		Architecture: recipe.Architecture, Forecast: architecture,
		Stages: []PlannedStage{{Name: "pretrain", Parameters: recipe.Stages[0].Parameters, PlannedTokens: 10_000_000}},
	})
	if err != nil {
		t.Fatal(err)
	}

	const sparkName = "GB10 Grace Blackwell (DGX Spark)"
	spark := map[int]HardwareConfiguration{}
	sawSingleNode := false
	for _, configuration := range report.Configurations {
		if configuration.Accelerator == sparkName {
			spark[configuration.GPUs] = configuration
			continue
		}
		if configuration.Nodes != 1 {
			t.Fatalf("single-node accelerator %s x%d reported %d nodes", configuration.Accelerator, configuration.GPUs, configuration.Nodes)
		}
		sawSingleNode = true
	}
	if !sawSingleNode {
		t.Fatal("expected at least one single-node accelerator configuration")
	}

	// 40 effective TFLOPS/node, scaled by the inter-node factors: 1 -> 40,
	// 2 -> 40*2*0.70 = 56, 4 -> 40*4*0.50 = 80.
	for gpus, wantEffective := range map[int]float64{1: 40, 2: 56, 4: 80} {
		configuration, ok := spark[gpus]
		if !ok {
			t.Fatalf("no DGX Spark configuration for %d GPU(s)", gpus)
		}
		if configuration.Nodes != gpus {
			t.Fatalf("DGX Spark x%d: nodes = %d, want %d (one GPU per node)", gpus, configuration.Nodes, gpus)
		}
		if math.Abs(configuration.EffectiveTFLOPS-wantEffective) > 1e-9 {
			t.Fatalf("DGX Spark x%d: effective = %g TFLOPS, want %g", gpus, configuration.EffectiveTFLOPS, wantEffective)
		}
	}
}
