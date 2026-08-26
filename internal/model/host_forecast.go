// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"fmt"

	"github.com/openwaldo/waldo/internal/training"
)

// HostForecast compares one portable workload with the execution environment
// selected by the same resolver used for training.
type HostForecast struct {
	Ready              bool               `json:"ready"`
	Reason             string             `json:"reason,omitempty"`
	Recommendation     string             `json:"recommendation,omitempty"`
	Execution          training.Execution `json:"execution"`
	AvailableMemory    uint64             `json:"available_memory_bytes"`
	RequiredMemory     uint64             `json:"required_memory_bytes"`
	ApproximateSeconds *int64             `json:"approximate_seconds,omitempty"`
	EstimateSource     string             `json:"estimate_source,omitempty"`
}

func (builder Builder) ResolveComposeBackend(ctx context.Context, compose Compose) (training.Selection, error) {
	plan, err := forecastPlanForCompose(compose)
	if err != nil {
		return training.Selection{}, err
	}
	return builder.ResolveBackend(ctx, plan.Architecture, planObjectives(plan))
}

func (builder Builder) ResolveIndexBackend(ctx context.Context, tokens int64) (training.Selection, error) {
	_, plan, err := planIndexSelection(tokens)
	if err != nil {
		return training.Selection{}, err
	}
	return builder.ResolveBackend(ctx, plan.Architecture, planObjectives(plan))
}

func AssessComposeHost(compose Compose, report ResourceForecast, execution training.Execution, hostMemory uint64) (HostForecast, error) {
	plan, err := forecastPlanForCompose(compose)
	if err != nil {
		return HostForecast{}, err
	}
	return assessHost(plan, report, execution, hostMemory)
}

func AssessIndexHost(tokens int64, report ResourceForecast, execution training.Execution, hostMemory uint64) (HostForecast, error) {
	_, plan, err := planIndexSelection(tokens)
	if err != nil {
		return HostForecast{}, err
	}
	return assessHost(plan, report, execution, hostMemory)
}

func assessHost(plan Plan, report ResourceForecast, execution training.Execution, hostMemory uint64) (HostForecast, error) {
	assessment := HostForecast{Execution: execution}
	if execution.Backend.Name == training.BackendFake {
		assessment.Reason = "the fake backend simulates training and does not produce a trainable model"
		return assessment, nil
	}
	devices := len(execution.Accelerators)
	shards := devices
	available := hostMemory
	if shards == 0 {
		shards = 1
	} else {
		available = execution.Accelerators[0].MemoryBytes
		for _, accelerator := range execution.Accelerators[1:] {
			if accelerator.MemoryBytes < available {
				available = accelerator.MemoryBytes
			}
		}
	}
	required, err := requiredMemoryPerGPU(plan, shards)
	if err != nil {
		return HostForecast{}, err
	}
	assessment.AvailableMemory = available
	assessment.RequiredMemory = required
	if available == 0 {
		assessment.Reason = "WALDO could not determine available training memory on this host"
		return assessment, nil
	}
	usable := available - available/10
	if required > usable {
		assessment.Reason = fmt.Sprintf("training requires %s per device including workspace, but this host has %s", forecastMemory(required), forecastMemory(available))
		assessment.Recommendation = fmt.Sprintf("use remote compute with at least %s of usable memory per device, or reduce the model, batch size, or sequence length", forecastMemory(required))
		return assessment, nil
	}
	assessment.Ready = true
	if devices == 0 || !homogeneousAccelerators(execution.Accelerators) {
		return assessment, nil
	}
	key := acceleratorKey(execution.Accelerators[0].Manufacturer, execution.Accelerators[0].Model)
	for _, configuration := range report.Configurations {
		if configuration.GPUs != devices || acceleratorKey(configuration.Manufacturer, configuration.Accelerator) != key {
			continue
		}
		seconds := configuration.ApproximateSeconds
		assessment.ApproximateSeconds = &seconds
		assessment.EstimateSource = configuration.EstimateSource
		break
	}
	return assessment, nil
}

func homogeneousAccelerators(accelerators []training.Accelerator) bool {
	if len(accelerators) == 0 {
		return false
	}
	key := acceleratorKey(accelerators[0].Manufacturer, accelerators[0].Model)
	for _, accelerator := range accelerators[1:] {
		if acceleratorKey(accelerator.Manufacturer, accelerator.Model) != key {
			return false
		}
	}
	return true
}

func forecastMemory(bytes uint64) string {
	const gibibyte = uint64(1 << 30)
	if bytes%gibibyte == 0 {
		return fmt.Sprintf("%d GiB", bytes/gibibyte)
	}
	return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gibibyte))
}
