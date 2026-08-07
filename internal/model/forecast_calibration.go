// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

type forecastRunEvidence struct {
	ModelID       string  `json:"model_id"`
	RunID         string  `json:"run_id"`
	Manufacturer  string  `json:"manufacturer"`
	Accelerator   string  `json:"accelerator"`
	GPUs          int     `json:"gpus"`
	TrainingFLOPs float64 `json:"training_flops"`
	ActiveSeconds float64 `json:"active_seconds"`
}

// LoadForecastCalibration verifies completed models beneath root and derives
// empirical throughput only from real accelerator runs with bounded timing.
func LoadForecastCalibration(root string) ([]ForecastCalibration, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var evidence []forecastRunEvidence
	for _, entry := range entries {
		if !entry.IsDir() || !validName.MatchString(entry.Name()) {
			continue
		}
		inspection, err := Inspect(root, entry.Name())
		if err != nil {
			// Calibration is optional planning evidence. An unrelated damaged
			// model must not make the versioned catalog unavailable, and no
			// facts from a model that fails inspection are admitted.
			continue
		}
		evidence = append(evidence, forecastEvidence(inspection)...)
	}
	return aggregateForecastEvidence(evidence)
}

func forecastEvidence(inspection Inspection) []forecastRunEvidence {
	var result []forecastRunEvidence
	for index, run := range inspection.Runs {
		if run.State != RunComplete || run.Observation == nil || run.Observation.Simulated || run.Observation.ConsumedTokens <= 0 || index >= len(inspection.RunBOMs) {
			continue
		}
		execution := inspection.RunBOMs[index].Execution
		if len(execution.Accelerators) == 0 {
			continue
		}
		manufacturer := execution.Accelerators[0].Manufacturer
		accelerator := execution.Accelerators[0].Model
		key := acceleratorKey(manufacturer, accelerator)
		if key == "" {
			continue
		}
		uniform := true
		for _, candidate := range execution.Accelerators[1:] {
			if acceleratorKey(candidate.Manufacturer, candidate.Model) != key {
				uniform = false
				break
			}
		}
		seconds := activeRunSeconds(run)
		if !uniform || seconds <= 0 {
			continue
		}
		flops := 6 * float64(inspection.Model.Forecast.ApproximateParameters) * float64(run.Observation.ConsumedTokens)
		if flops <= 0 {
			continue
		}
		result = append(result, forecastRunEvidence{
			ModelID: inspection.Model.ID, RunID: run.ID,
			Manufacturer: manufacturer, Accelerator: accelerator, GPUs: len(execution.Accelerators),
			TrainingFLOPs: flops, ActiveSeconds: seconds,
		})
	}
	return result
}

func activeRunSeconds(run RunRecord) float64 {
	var total float64
	for _, attempt := range run.Attempts {
		started, startErr := time.Parse(time.RFC3339Nano, attempt.Started)
		finished, finishErr := time.Parse(time.RFC3339Nano, attempt.Finished)
		if startErr == nil && finishErr == nil && finished.After(started) {
			total += finished.Sub(started).Seconds()
		}
	}
	if total > 0 || len(run.Attempts) > 0 {
		return total
	}
	started, startErr := time.Parse(time.RFC3339Nano, run.Started)
	finished, finishErr := time.Parse(time.RFC3339Nano, run.Finished)
	if startErr != nil || finishErr != nil || !finished.After(started) {
		return 0
	}
	return finished.Sub(started).Seconds()
}

func aggregateForecastEvidence(evidence []forecastRunEvidence) ([]ForecastCalibration, error) {
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].ModelID != evidence[j].ModelID {
			return evidence[i].ModelID < evidence[j].ModelID
		}
		return evidence[i].RunID < evidence[j].RunID
	})
	type aggregate struct {
		calibration ForecastCalibration
		evidence    []forecastRunEvidence
	}
	groups := map[string]*aggregate{}
	for _, item := range evidence {
		key := fmt.Sprintf("%s/%d", acceleratorKey(item.Manufacturer, item.Accelerator), item.GPUs)
		group := groups[key]
		if group == nil {
			group = &aggregate{calibration: ForecastCalibration{
				Schema: forecastCalibrationSchema, Manufacturer: item.Manufacturer,
				Accelerator: item.Accelerator, GPUs: item.GPUs,
			}}
			groups[key] = group
		}
		group.calibration.Runs++
		group.calibration.TrainingFLOPs += item.TrainingFLOPs
		group.calibration.ActiveSeconds += item.ActiveSeconds
		group.evidence = append(group.evidence, item)
	}
	result := make([]ForecastCalibration, 0, len(groups))
	for _, group := range groups {
		group.calibration.EffectiveTFLOPS = group.calibration.TrainingFLOPs / group.calibration.ActiveSeconds / 1e12
		digest, err := hashJSON(group.evidence)
		if err != nil {
			return nil, err
		}
		group.calibration.EvidenceSHA256 = digest
		result = append(result, group.calibration)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Manufacturer != right.Manufacturer {
			return left.Manufacturer < right.Manufacturer
		}
		if left.Accelerator != right.Accelerator {
			return left.Accelerator < right.Accelerator
		}
		return left.GPUs < right.GPUs
	})
	return result, nil
}

func acceleratorKey(manufacturer, accelerator string) string {
	value := strings.ToLower(manufacturer + " " + accelerator)
	for _, family := range []string{"m4 max", "m5 max", "rtx pro 6000", "rtx 5090", "h100", "h200", "mi325x", "mi350x"} {
		if strings.Contains(value, family) {
			return strings.ReplaceAll(family, " ", "")
		}
	}
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return character
		}
		return -1
	}, value)
}
