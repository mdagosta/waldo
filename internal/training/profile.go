// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"fmt"
	"math"
)

const (
	DefaultProfile            = "causal-pretrain-v1"
	BalancedProfile           = "causal-pretrain-v2"
	ProfileSchema             = 1
	defaultShuffleBufferBytes = int64(64 * 1024 * 1024)
)

func ResolveParameters(parameters Parameters) (ResolvedParameters, error) {
	profile := parameters.Profile
	if profile == "" {
		profile = DefaultProfile
	}
	if profile != DefaultProfile && profile != BalancedProfile {
		return ResolvedParameters{}, fmt.Errorf("unsupported training profile %q", profile)
	}
	if parameters.Steps <= 0 || parameters.BatchSize <= 0 || parameters.SequenceLength <= 0 || parameters.LearningRate <= 0 || math.IsNaN(parameters.LearningRate) || math.IsInf(parameters.LearningRate, 0) {
		return ResolvedParameters{}, fmt.Errorf("steps, batch_size, sequence_length, and learning_rate must be finite and positive")
	}
	epochs := parameters.Epochs
	if epochs == 0 {
		epochs = 1
	}
	if epochs < 1 || epochs > 1_000_000 {
		return ResolvedParameters{}, fmt.Errorf("epochs must be in 1..1000000")
	}
	capacity, overflow := multiplyInt64(parameters.Steps, parameters.BatchSize, parameters.SequenceLength)
	if overflow {
		return ResolvedParameters{}, fmt.Errorf("planned token capacity overflows int64")
	}
	weightDecay := 0.1
	if parameters.WeightDecay != nil {
		weightDecay = *parameters.WeightDecay
	}
	if weightDecay < 0 || weightDecay > 1 || math.IsNaN(weightDecay) || math.IsInf(weightDecay, 0) {
		return ResolvedParameters{}, fmt.Errorf("weight_decay must be finite and in 0..1")
	}
	warmup := min(int64(100), parameters.Steps/10)
	if parameters.Steps > 1 && warmup == 0 {
		warmup = 1
	}
	if parameters.WarmupSteps != nil {
		warmup = *parameters.WarmupSteps
	}
	checkpointEvery := min(int64(500), parameters.Steps)
	if parameters.CheckpointEvery != nil {
		checkpointEvery = *parameters.CheckpointEvery
	}
	evaluateEvery := min(int64(500), parameters.Steps)
	if parameters.EvaluateEvery != nil {
		evaluateEvery = *parameters.EvaluateEvery
	}
	if warmup < 0 || warmup > parameters.Steps {
		return ResolvedParameters{}, fmt.Errorf("warmup_steps must be in 0..steps")
	}
	if checkpointEvery < 0 || checkpointEvery > parameters.Steps {
		return ResolvedParameters{}, fmt.Errorf("checkpoint_every must be in 0..steps")
	}
	if evaluateEvery < 0 || evaluateEvery > parameters.Steps {
		return ResolvedParameters{}, fmt.Errorf("evaluate_every must be in 0..steps")
	}
	shuffleBuffer := 1024
	if parameters.ShuffleBufferRecords != nil {
		shuffleBuffer = *parameters.ShuffleBufferRecords
	}
	if shuffleBuffer < 1 || shuffleBuffer > 1_000_000 {
		return ResolvedParameters{}, fmt.Errorf("shuffle_buffer_records must be in 1..1000000")
	}
	shuffleBufferBytes := defaultShuffleBufferBytes
	if parameters.ShuffleBufferBytes != nil {
		shuffleBufferBytes = *parameters.ShuffleBufferBytes
	}
	if shuffleBufferBytes < 1 || shuffleBufferBytes > 16*1024*1024*1024 {
		return ResolvedParameters{}, fmt.Errorf("shuffle_buffer_bytes must be in 1..17179869184")
	}
	evaluationFraction := 0.01
	if parameters.EvaluationFraction != nil {
		evaluationFraction = *parameters.EvaluationFraction
	}
	if evaluationFraction < 0 || evaluationFraction >= 1 || math.IsNaN(evaluationFraction) || math.IsInf(evaluationFraction, 0) {
		return ResolvedParameters{}, fmt.Errorf("evaluation_fraction must be finite and in 0..<1")
	}
	evaluationMaxRecords := 256
	if parameters.EvaluationMaxRecords != nil {
		evaluationMaxRecords = *parameters.EvaluationMaxRecords
	}
	if evaluationMaxRecords < 0 || evaluationMaxRecords > 1_000_000 {
		return ResolvedParameters{}, fmt.Errorf("evaluation_max_records must be in 0..1000000")
	}
	evaluationMaxBytes := int64(1 * 1024 * 1024)
	if parameters.EvaluationMaxBytes != nil {
		evaluationMaxBytes = *parameters.EvaluationMaxBytes
	}
	if evaluationMaxBytes < 0 || evaluationMaxBytes > 16*1024*1024*1024 {
		return ResolvedParameters{}, fmt.Errorf("evaluation_max_bytes must be in 0..17179869184")
	}
	if evaluationFraction == 0 || evaluationMaxRecords == 0 || evaluationMaxBytes == 0 {
		evaluationFraction, evaluationMaxRecords, evaluationMaxBytes = 0, 0, 0
	}
	order := "bounded-shuffle-v1"
	selection := "lowest-sha256-v1"
	profileSchema := ProfileSchema
	if profile == BalancedProfile {
		order = "corpus-balanced-shuffle-v1"
		selection = "stratified-lowest-sha256-v1"
		profileSchema = 2
	}
	return ResolvedParameters{
		Profile: profile, ProfileSchema: profileSchema,
		Epochs: epochs, Steps: parameters.Steps, BatchSize: parameters.BatchSize,
		SequenceLength: parameters.SequenceLength, LearningRate: parameters.LearningRate,
		Seed: parameters.Seed, PlannedTokenCapacity: capacity,
		Optimizer:       Optimizer{Name: "adamw", WeightDecay: weightDecay, Beta1: 0.9, Beta2: 0.95, Epsilon: 1e-8},
		Schedule:        Schedule{Name: "cosine", WarmupSteps: warmup, MinimumRateRatio: 0.1},
		Data:            DataPlan{Order: order, ShuffleBufferRecords: shuffleBuffer, ShuffleBufferBytes: shuffleBufferBytes, Packing: "continuous-eos-v1"},
		Evaluation:      &EvaluationPolicy{Selection: selection, Fraction: evaluationFraction, MaxRecords: evaluationMaxRecords, MaxBytes: evaluationMaxBytes},
		CheckpointEvery: checkpointEvery, EvaluateEvery: evaluateEvery,
	}, nil
}

func multiplyInt64(values ...int64) (int64, bool) {
	result := int64(1)
	for _, value := range values {
		if value <= 0 || result > math.MaxInt64/value {
			return 0, true
		}
		result *= value
	}
	return result, false
}
