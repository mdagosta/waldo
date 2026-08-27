// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"testing"

	"github.com/openwaldo/waldo/internal/training"
)

func TestAssessComposeHostUsesResolvedAcceleratorAndCatalogEstimate(t *testing.T) {
	compose := validCompose()
	report, err := ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	execution := training.Execution{
		Backend:      training.Identity{Name: training.BackendPyTorch, Revision: "pytorch-v1"},
		Host:         training.Host{OS: "linux", Architecture: "amd64"},
		Accelerators: []training.Accelerator{{Manufacturer: "NVIDIA", Model: "H100 SXM", MemoryBytes: 80 << 30}},
		Nodes:        1, WorldSize: 1,
	}
	assessment, err := AssessComposeHost(compose, report, execution, 256<<30)
	if err != nil {
		t.Fatal(err)
	}
	if !assessment.Ready || assessment.Reason != "" || assessment.RequiredMemory == 0 || assessment.AvailableMemory != 80<<30 || assessment.ApproximateSeconds == nil || assessment.EstimateSource != "catalog" {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestAssessComposeHostExplainsInsufficientCPUMemory(t *testing.T) {
	compose := validCompose()
	report, err := ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	execution := training.Execution{
		Backend: training.Identity{Name: training.BackendPyTorch, Revision: "pytorch-v1"},
		Host:    training.Host{OS: "linux", Architecture: "amd64"}, Nodes: 1, WorldSize: 1,
	}
	assessment, err := AssessComposeHost(compose, report, execution, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Ready || assessment.Reason == "" || assessment.Recommendation == "" || assessment.AvailableMemory != 1<<30 || assessment.RequiredMemory == 0 {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestAssessComposeHostDoesNotTreatFakeBackendAsReady(t *testing.T) {
	compose := validCompose()
	report, err := ForecastCompose(compose)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := AssessComposeHost(compose, report, training.Execution{
		Backend: training.Identity{Name: training.BackendFake, Revision: "fake-v1"},
		Host:    training.Host{OS: "linux", Architecture: "amd64"}, Nodes: 1, WorldSize: 1,
	}, 256<<30)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Ready || assessment.Reason == "" {
		t.Fatalf("assessment = %+v", assessment)
	}
}
