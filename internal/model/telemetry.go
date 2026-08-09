// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/openwaldo/waldo/internal/training"
)

const TelemetryFilename = "TELEMETRY.csv"

var telemetryHeader = []string{
	"observed_utc", "elapsed_seconds", "run_id", "stage", "attempt",
	"event", "state", "step", "planned_steps", "tokens", "planned_tokens", "loss",
	"heldout_loss", "heldout_perplexity", "learning_rate", "tokens_per_second", "eta_seconds", "message",
}

type telemetryRow struct {
	Observed      time.Time
	Started       time.Time
	RunID         string
	Stage         string
	Attempt       int
	Event         string
	State         RunState
	PlannedSteps  int64
	PlannedTokens int64
	Training      *training.Event
	Message       string
}

func appendTelemetry(path string, row telemetryRow) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	writer := csv.NewWriter(file)
	if info.Size() == 0 {
		if err := writer.Write(telemetryHeader); err != nil {
			return err
		}
	}
	if err := writer.Write(telemetryRecord(row)); err != nil {
		return err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return file.Sync()
}

func telemetryRecord(row telemetryRow) []string {
	elapsed := row.Observed.Sub(row.Started).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	values := []string{
		formatTime(row.Observed), strconv.FormatFloat(elapsed, 'f', 3, 64), row.RunID, row.Stage,
		strconv.Itoa(row.Attempt), row.Event, string(row.State), "", strconv.FormatInt(row.PlannedSteps, 10),
		"", strconv.FormatInt(row.PlannedTokens, 10), "", "", "", "", "", "", row.Message,
	}
	if row.Training == nil {
		return values
	}
	event := row.Training
	values[7] = optionalInt(event.Step)
	values[9] = optionalInt(event.Tokens)
	if event.Loss != nil {
		values[11] = strconv.FormatFloat(*event.Loss, 'g', -1, 64)
	}
	if event.Evaluation != nil {
		values[12] = optionalMetric(event.Evaluation.Metrics, "heldout_loss")
		values[13] = optionalMetric(event.Evaluation.Metrics, "heldout_perplexity")
	}
	if event.LearningRate > 0 {
		values[14] = strconv.FormatFloat(event.LearningRate, 'g', -1, 64)
	}
	if event.TokensPerSecond > 0 {
		values[15] = strconv.FormatFloat(event.TokensPerSecond, 'g', -1, 64)
	}
	values[16] = optionalInt(event.ETASeconds)
	return values
}

func optionalInt(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func optionalMetric(metrics map[string]float64, name string) string {
	value, ok := metrics[name]
	if !ok {
		return ""
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func telemetryError(path string, err error) error {
	return fmt.Errorf("append training telemetry %s: %w", path, err)
}
