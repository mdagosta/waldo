// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package status reports whether the current machine is ready for WALDO data
// and model workflows without changing configuration or durable state.
package status

import (
	"context"
	"fmt"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/host"
	waldoindex "github.com/openwaldo/waldo/internal/index"
	"github.com/openwaldo/waldo/internal/model"
	"github.com/openwaldo/waldo/internal/training"
)

type Readiness struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

type Index struct {
	Readiness
	Path    string `json:"path"`
	Managed bool   `json:"managed"`
}

type Lookaside struct {
	Cache   string          `json:"cache"`
	Scratch string          `json:"scratch"`
	Publish *config.Publish `json:"publish,omitempty"`
}

type Training struct {
	Readiness
	Execution *training.Execution `json:"execution,omitempty"`
}

type Report struct {
	Host      host.Facts `json:"host"`
	Index     Index      `json:"index"`
	Lookaside Lookaside  `json:"lookaside"`
	Training  Training   `json:"training"`
	Ready     bool       `json:"ready"`
	Reasons   []string   `json:"reasons,omitempty"`
}

func Inspect(ctx context.Context) (Report, error) {
	facts, err := host.Inspect()
	if err != nil {
		return Report{}, err
	}
	configuration, err := config.Load()
	if err != nil {
		return Report{}, err
	}
	indexRoot, managed, err := config.EffectiveIndexRoot(configuration)
	if err != nil {
		return Report{}, err
	}
	cache, err := config.EffectiveCacheRoot(configuration)
	if err != nil {
		return Report{}, err
	}
	scratch, err := config.EffectiveScratchRoot(configuration)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Host:      facts,
		Index:     Index{Path: indexRoot, Managed: managed},
		Lookaside: Lookaside{Cache: cache, Scratch: scratch, Publish: configuration.Lookaside.Publish},
	}
	if _, err := waldoindex.ResolveConfigured(indexRoot, ""); err != nil {
		report.Index.Reason = err.Error()
		report.Reasons = append(report.Reasons, "index: "+err.Error())
	} else {
		report.Index.Ready = true
	}
	preset, err := model.PresetByName("10m")
	if err != nil {
		return Report{}, err
	}
	builder := model.Builder{Resolver: training.NewEnvironmentResolver(config.EffectiveModelBackend(configuration))}
	selection, err := builder.ResolveBackend(ctx, preset.Architecture, []string{"causal-language-modeling"})
	if err != nil {
		report.Training.Reason = err.Error()
		report.Reasons = append(report.Reasons, "training: "+err.Error())
	} else {
		report.Training.Execution = &selection.Execution
		if selection.Execution.Backend.Name == training.BackendFake {
			report.Training.Reason = "the fake backend simulates training and does not produce a trainable model"
			report.Reasons = append(report.Reasons, "training: "+report.Training.Reason)
		} else {
			report.Training.Ready = true
		}
	}
	report.Ready = report.Index.Ready && report.Training.Ready
	if !report.Ready && len(report.Reasons) == 0 {
		return Report{}, fmt.Errorf("status is not ready without an explanation")
	}
	return report, nil
}
