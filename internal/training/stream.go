// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"github.com/openwaldo/waldo/internal/shard"
)

type Record struct {
	SelectionID string `json:"selection_id"`
	ID          string `json:"id"`
	Text        string `json:"text"`
	Source      string `json:"source"`
	License     string `json:"license"`
	Language    string `json:"language,omitempty"`
	Corpus      string `json:"corpus,omitempty"`
	Tokens      []int  `json:"tokens,omitempty"`
}

type RecordPartition struct {
	Evaluation EvaluationSet
	selected   map[string]bool
	inputs     []Input
	parameters ResolvedParameters
	codec      TokenCodec
}

type PartitionProgress struct {
	CurrentShard int
	TotalShards  int
	Records      int64
	Bytes        int64
	TotalBytes   int64
}

type evaluationCandidate struct {
	key       string
	corpus    string
	score     [32]byte
	textBytes int64
	tokens    int64
}

type evaluationHeap []evaluationCandidate

func (values evaluationHeap) Len() int { return len(values) }
func (values evaluationHeap) Less(i, j int) bool {
	return string(values[i].score[:]) > string(values[j].score[:])
}
func (values evaluationHeap) Swap(i, j int)   { values[i], values[j] = values[j], values[i] }
func (values *evaluationHeap) Push(value any) { *values = append(*values, value.(evaluationCandidate)) }
func (values *evaluationHeap) Pop() any {
	old := *values
	last := old[len(old)-1]
	*values = old[:len(old)-1]
	return last
}

func NewRecordPartition(inputs []Input, parameters ResolvedParameters) (RecordPartition, error) {
	return NewRecordPartitionContext(context.Background(), inputs, parameters, nil)
}

// NewRecordPartitionWithProgress deterministically selects held-out records
// while reporting the otherwise CPU-heavy full-corpus scan.
func NewRecordPartitionWithProgress(inputs []Input, parameters ResolvedParameters, progress func(PartitionProgress)) (RecordPartition, error) {
	return NewRecordPartitionContext(context.Background(), inputs, parameters, progress)
}

// NewRecordPartitionContext makes the full-corpus held-out selection scan
// interruptible by its caller.
func NewRecordPartitionContext(ctx context.Context, inputs []Input, parameters ResolvedParameters, progress func(PartitionProgress)) (RecordPartition, error) {
	return NewRecordPartitionContextWithTokenizer(ctx, inputs, parameters, byteCodec{}, progress)
}

func NewRecordPartitionContextWithTokenizer(ctx context.Context, inputs []Input, parameters ResolvedParameters, codec TokenCodec, progress func(PartitionProgress)) (RecordPartition, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if codec == nil {
		return RecordPartition{}, fmt.Errorf("record partition requires a tokenizer")
	}
	ordered := orderedInputs(inputs)
	partition := RecordPartition{selected: make(map[string]bool), inputs: ordered, parameters: parameters, codec: codec}
	policy := parameters.Evaluation
	if policy == nil {
		policy = &EvaluationPolicy{Selection: "none-v1"}
	}
	if policy.Fraction == 0 || policy.MaxRecords == 0 || policy.MaxBytes == 0 {
		partition.Evaluation = EvaluationSet{Selection: policy.Selection, Seed: parameters.Seed, SHA256: emptyEvaluationDigest()}
		return partition, nil
	}
	var candidates evaluationHeap
	heap.Init(&candidates)
	groupCandidates := map[string]*evaluationHeap{}
	var records, completedBytes, totalBytes int64
	for _, input := range ordered {
		totalBytes += input.Bytes
	}
	for inputPosition, input := range ordered {
		err := shard.WalkRecords(input.Path, func(row int64, view shard.RecordView) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			records++
			key := selectionID(input.SHA256, row)
			score := evaluationScore(parameters.Seed, key)
			candidate := evaluationCandidate{key: key, corpus: input.Corpus, score: score, textBytes: int64(len(view.Text)), tokens: int64(codec.Count(view.Text))}
			candidateHeap := &candidates
			if policy.Selection == "stratified-lowest-sha256-v1" {
				candidateHeap = groupCandidates[input.Corpus]
				if candidateHeap == nil {
					candidateHeap = &evaluationHeap{}
					heap.Init(candidateHeap)
					groupCandidates[input.Corpus] = candidateHeap
				}
			}
			if candidateHeap.Len() < policy.MaxRecords {
				heap.Push(candidateHeap, candidate)
			} else if string(score[:]) < string((*candidateHeap)[0].score[:]) {
				heap.Pop(candidateHeap)
				heap.Push(candidateHeap, candidate)
			}
			if progress != nil && records%1000 == 0 {
				progress(PartitionProgress{CurrentShard: inputPosition + 1, TotalShards: len(ordered), Records: records, Bytes: completedBytes, TotalBytes: totalBytes})
			}
			return nil
		})
		if err != nil {
			return RecordPartition{}, fmt.Errorf("select evaluation records from shard %s: %w", input.SHA256, err)
		}
		completedBytes += input.Bytes
		if progress != nil {
			progress(PartitionProgress{CurrentShard: inputPosition + 1, TotalShards: len(ordered), Records: records, Bytes: completedBytes, TotalBytes: totalBytes})
		}
	}
	desired := int(math.Ceil(float64(records) * policy.Fraction))
	if records < 2 {
		desired = 0
	} else {
		desired = min(desired, policy.MaxRecords, int(records-1))
		if desired < 1 {
			desired = 1
		}
	}
	var selected []evaluationCandidate
	var bytes int64
	if policy.Selection == "stratified-lowest-sha256-v1" {
		groups := make([]string, 0, len(groupCandidates))
		values := make(map[string][]evaluationCandidate, len(groupCandidates))
		maxRounds := 0
		for group, candidates := range groupCandidates {
			groups = append(groups, group)
			values[group] = append([]evaluationCandidate(nil), (*candidates)...)
			sort.Slice(values[group], func(i, j int) bool { return string(values[group][i].score[:]) < string(values[group][j].score[:]) })
			maxRounds = max(maxRounds, len(values[group]))
		}
		sort.Strings(groups)
		for round := 0; round < maxRounds && len(selected) < desired; round++ {
			for _, group := range groups {
				if round >= len(values[group]) {
					continue
				}
				candidate := values[group][round]
				if candidate.textBytes > policy.MaxBytes-bytes {
					continue
				}
				selected = append(selected, candidate)
				bytes += candidate.textBytes
				if len(selected) == desired {
					break
				}
			}
		}
	} else {
		values := append([]evaluationCandidate(nil), candidates...)
		sort.Slice(values, func(i, j int) bool { return string(values[i].score[:]) < string(values[j].score[:]) })
		for _, candidate := range values {
			if len(selected) == desired {
				break
			}
			if candidate.textBytes > policy.MaxBytes-bytes {
				continue
			}
			selected = append(selected, candidate)
			bytes += candidate.textBytes
		}
	}
	if desired > 0 && len(selected) == 0 {
		return RecordPartition{}, fmt.Errorf("no held-out record fits evaluation_max_bytes=%d; increase the limit or explicitly disable evaluation", policy.MaxBytes)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].key < selected[j].key })
	hasher := sha256.New()
	var tokenTargets int64
	for _, candidate := range selected {
		partition.selected[candidate.key] = true
		_, _ = fmt.Fprintln(hasher, candidate.key)
		tokenTargets += candidate.tokens
	}
	partition.Evaluation = EvaluationSet{
		Selection: policy.Selection, Seed: parameters.Seed, Records: int64(len(selected)),
		TokenTargets: tokenTargets, TextBytes: bytes, SHA256: hex.EncodeToString(hasher.Sum(nil)),
	}
	return partition, nil
}

func (partition RecordPartition) TrainingRecords() (RecordSource, error) {
	source, err := NewCanonicalRecordSource(partition.inputs, partition.parameters)
	if err != nil {
		return nil, err
	}
	return filteredRecordSource{source: source, include: func(record Record) bool { return !partition.selected[record.SelectionID] }}, nil
}

func (partition RecordPartition) EvaluationRecords() RecordSource {
	return rawRecordSource{inputs: partition.inputs, include: func(record Record) bool { return partition.selected[record.SelectionID] }}
}

func (partition RecordPartition) TrainingByteTargets(ctx context.Context) (int64, error) {
	var perEpoch int64
	err := rawRecordSource{inputs: partition.inputs, include: func(record Record) bool { return !partition.selected[record.SelectionID] }}.Stream(ctx, func(record Record) error {
		value := int64(partition.codec.Count(record.Text)) + 1
		if perEpoch > math.MaxInt64-value {
			return fmt.Errorf("training byte-token target count overflows int64")
		}
		perEpoch += value
		return nil
	})
	if err != nil {
		return 0, err
	}
	if perEpoch < 2 || perEpoch > math.MaxInt64/partition.parameters.Epochs {
		return 0, fmt.Errorf("held-out partition leaves no usable training targets")
	}
	return perEpoch*partition.parameters.Epochs - 1, nil
}

type filteredRecordSource struct {
	source  RecordSource
	include func(Record) bool
}

func (source filteredRecordSource) Stream(ctx context.Context, consume func(Record) error) error {
	return source.source.Stream(ctx, func(record Record) error {
		if !source.include(record) {
			return nil
		}
		return consume(record)
	})
}

type rawRecordSource struct {
	inputs  []Input
	include func(Record) bool
}

func (source rawRecordSource) Stream(ctx context.Context, consume func(Record) error) error {
	for _, input := range source.inputs {
		err := shard.WalkRecords(input.Path, func(row int64, view shard.RecordView) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			record := recordFromView(input, row, view)
			if source.include != nil && !source.include(record) {
				return nil
			}
			return consume(record)
		})
		if err != nil {
			return fmt.Errorf("stream canonical shard %s: %w", input.SHA256, err)
		}
	}
	return nil
}

func selectionID(shardHash string, row int64) string { return fmt.Sprintf("%s:%d", shardHash, row) }

func evaluationScore(seed uint64, key string) [32]byte {
	value := make([]byte, 8+len(key))
	binary.BigEndian.PutUint64(value, seed)
	copy(value[8:], key)
	return sha256.Sum256(value)
}

func emptyEvaluationDigest() string {
	digest := sha256.Sum256(nil)
	return hex.EncodeToString(digest[:])
}

type RecordSource interface {
	Stream(context.Context, func(Record) error) error
}

// CountByteTargets returns the exact number of next-token targets produced by
// continuous-EOS packing with the built-in byte tokenizer. The first token in
// the continuous stream is context rather than a prediction target.
func CountByteTargets(ctx context.Context, inputs []Input) (int64, error) {
	var tokens int64
	for _, input := range inputs {
		err := shard.WalkRecords(input.Path, func(_ int64, view shard.RecordView) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			recordTokens := int64(len(view.Text)) + 1 // UTF-8 bytes plus EOS.
			if tokens > math.MaxInt64-recordTokens {
				return fmt.Errorf("byte-token target count overflows int64")
			}
			tokens += recordTokens
			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("count byte tokens in shard %s: %w", input.SHA256, err)
		}
	}
	if tokens < 2 {
		return 0, fmt.Errorf("canonical stream contains no byte-token prediction targets")
	}
	return tokens - 1, nil
}

// ByteTargetsForEpochs expands a one-epoch next-token target count while
// preserving continuous packing across epoch boundaries. CountByteTargets
// excludes the stream's first token once, not once per epoch.
func ByteTargetsForEpochs(oneEpochTargets, epochs int64) (int64, error) {
	if oneEpochTargets < 1 || epochs < 1 {
		return 0, fmt.Errorf("byte targets and epochs must be positive")
	}
	tokensPerEpoch := oneEpochTargets + 1
	if tokensPerEpoch <= 0 || tokensPerEpoch > math.MaxInt64/epochs {
		return 0, fmt.Errorf("epoch byte-token target count overflows int64")
	}
	return tokensPerEpoch*epochs - 1, nil
}

type canonicalRecordSource struct {
	inputs      []Input
	seed        uint64
	epochs      int64
	buffer      int
	bufferBytes int64
	balanced    bool
}

func NewCanonicalRecordSource(inputs []Input, parameters ResolvedParameters) (RecordSource, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("canonical record source requires at least one shard input")
	}
	if parameters.Epochs < 1 {
		return nil, fmt.Errorf("canonical record source requires at least one epoch")
	}
	if (parameters.Data.Order != "bounded-shuffle-v1" && parameters.Data.Order != "corpus-balanced-shuffle-v1") || parameters.Data.ShuffleBufferRecords < 1 || parameters.Data.ShuffleBufferBytes < 1 {
		return nil, fmt.Errorf("unsupported canonical record order %q", parameters.Data.Order)
	}
	ordered := orderedInputs(inputs)
	return &canonicalRecordSource{inputs: ordered, seed: parameters.Seed, epochs: parameters.Epochs, buffer: parameters.Data.ShuffleBufferRecords, bufferBytes: parameters.Data.ShuffleBufferBytes, balanced: parameters.Data.Order == "corpus-balanced-shuffle-v1"}, nil
}

func orderedInputs(inputs []Input) []Input {
	ordered := append([]Input(nil), inputs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SHA256 != ordered[j].SHA256 {
			return ordered[i].SHA256 < ordered[j].SHA256
		}
		return ordered[i].Path < ordered[j].Path
	})
	return ordered
}

func (source *canonicalRecordSource) Stream(ctx context.Context, consume func(Record) error) error {
	if consume == nil {
		return fmt.Errorf("record consumer is required")
	}
	for epoch := int64(0); epoch < source.epochs; epoch++ {
		if err := source.streamEpoch(ctx, epoch, consume); err != nil {
			return err
		}
	}
	return nil
}

func (source *canonicalRecordSource) streamEpoch(ctx context.Context, epoch int64, consume func(Record) error) error {
	if source.balanced {
		return source.streamBalancedEpoch(ctx, epoch, consume)
	}
	return source.streamInputGroup(ctx, epoch, source.inputs, consume)
}

func (source *canonicalRecordSource) streamInputGroup(ctx context.Context, epoch int64, group []Input, consume func(Record) error) error {
	inputs := append([]Input(nil), group...)
	inputRandom := newSplitMix64(source.seed + uint64(epoch)*0x9e3779b97f4a7c15)
	for index := len(inputs) - 1; index > 0; index-- {
		other := inputRandom.intn(index + 1)
		inputs[index], inputs[other] = inputs[other], inputs[index]
	}
	random := newSplitMix64(source.seed ^ 0x6a09e667f3bcc909 ^ uint64(epoch)*0xbf58476d1ce4e5b9)
	buffer := make([]Record, 0, source.buffer)
	var bufferedBytes int64
	emit := func(record Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return consume(record)
	}
	for _, input := range inputs {
		err := shard.WalkRecords(input.Path, func(row int64, view shard.RecordView) error {
			record := recordFromView(input, row, view)
			recordBytes := recordMemoryBytes(record)
			if recordBytes > source.bufferBytes {
				return emit(record)
			}
			for len(buffer) > 0 && (len(buffer) >= source.buffer || bufferedBytes+recordBytes > source.bufferBytes) {
				position := random.intn(len(buffer))
				selected := buffer[position]
				if err := emit(selected); err != nil {
					return err
				}
				bufferedBytes -= recordMemoryBytes(selected)
				last := len(buffer) - 1
				buffer[position] = buffer[last]
				buffer = buffer[:last]
			}
			buffer = append(buffer, record)
			bufferedBytes += recordBytes
			return nil
		})
		if err != nil {
			return fmt.Errorf("stream canonical shard %s in epoch %d: %w", input.SHA256, epoch+1, err)
		}
	}
	for len(buffer) > 0 {
		position := random.intn(len(buffer))
		if err := emit(buffer[position]); err != nil {
			return err
		}
		last := len(buffer) - 1
		bufferedBytes -= recordMemoryBytes(buffer[position])
		buffer[position] = buffer[last]
		buffer = buffer[:last]
	}
	return nil
}

type balancedRecord struct {
	record Record
	err    error
}

// streamBalancedEpoch emits one record from each declared corpus group in
// stable round-robin order. Each group retains the bounded shuffle contract.
func (source *canonicalRecordSource) streamBalancedEpoch(ctx context.Context, epoch int64, consume func(Record) error) error {
	groups := map[string][]Input{}
	for _, input := range source.inputs {
		if input.Corpus == "" {
			return fmt.Errorf("corpus-balanced record order requires corpus identity for shard %s", input.SHA256)
		}
		groups[input.Corpus] = append(groups[input.Corpus], input)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	streamContext, cancel := context.WithCancel(ctx)
	defer cancel()
	streams := make([]<-chan balancedRecord, 0, len(names))
	for position, name := range names {
		output := make(chan balancedRecord)
		streams = append(streams, output)
		group := groups[name]
		go func(groupPosition int, values []Input, destination chan<- balancedRecord) {
			defer close(destination)
			groupSource := *source
			groupSource.seed ^= uint64(groupPosition+1) * 0x9e3779b97f4a7c15
			err := groupSource.streamInputGroup(streamContext, epoch, values, func(record Record) error {
				select {
				case destination <- balancedRecord{record: record}:
					return nil
				case <-streamContext.Done():
					return streamContext.Err()
				}
			})
			if err != nil && streamContext.Err() == nil {
				select {
				case destination <- balancedRecord{err: err}:
				case <-streamContext.Done():
				}
			}
		}(position, group, output)
	}
	for len(streams) > 0 {
		remaining := streams[:0]
		for _, stream := range streams {
			item, ok := <-stream
			if !ok {
				continue
			}
			if item.err != nil {
				return item.err
			}
			if err := consume(item.record); err != nil {
				return err
			}
			remaining = append(remaining, stream)
		}
		streams = remaining
	}
	return nil
}

func recordFromView(input Input, row int64, view shard.RecordView) Record {
	return Record{SelectionID: selectionID(input.SHA256, row), ID: view.ID, Text: view.Text, Source: view.Source, License: view.License, Language: view.Language, Corpus: input.Corpus}
}

// recordMemoryBytes accounts for retained string data and the five string
// headers in Record. It is deliberately conservative and keeps the shuffle
// window bounded by bytes as well as record count.
func recordMemoryBytes(record Record) int64 {
	return int64(7*16 + len(record.SelectionID) + len(record.ID) + len(record.Text) + len(record.Source) + len(record.License) + len(record.Language) + len(record.Corpus))
}

// splitMix64 is specified here rather than delegated to math/rand so record
// ordering remains stable across Go runtime revisions.
type splitMix64 struct{ state uint64 }

func newSplitMix64(seed uint64) *splitMix64 { return &splitMix64{state: seed} }

func (random *splitMix64) next() uint64 {
	random.state += 0x9e3779b97f4a7c15
	value := random.state
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func (random *splitMix64) intn(limit int) int {
	if limit <= 0 {
		panic("splitMix64.intn called with a non-positive limit")
	}
	return int(random.next() % uint64(limit))
}
