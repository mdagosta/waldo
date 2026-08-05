package training

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/openwaldo/waldo-new/internal/shard"
)

type Record struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Source   string `json:"source"`
	License  string `json:"license"`
	Language string `json:"language,omitempty"`
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
}

func NewCanonicalRecordSource(inputs []Input, parameters ResolvedParameters) (RecordSource, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("canonical record source requires at least one shard input")
	}
	if parameters.Epochs < 1 {
		return nil, fmt.Errorf("canonical record source requires at least one epoch")
	}
	if parameters.Data.Order != "bounded-shuffle-v1" || parameters.Data.ShuffleBufferRecords < 1 || parameters.Data.ShuffleBufferBytes < 1 {
		return nil, fmt.Errorf("unsupported canonical record order %q", parameters.Data.Order)
	}
	ordered := append([]Input(nil), inputs...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SHA256 != ordered[j].SHA256 {
			return ordered[i].SHA256 < ordered[j].SHA256
		}
		return ordered[i].Path < ordered[j].Path
	})
	return &canonicalRecordSource{inputs: ordered, seed: parameters.Seed, epochs: parameters.Epochs, buffer: parameters.Data.ShuffleBufferRecords, bufferBytes: parameters.Data.ShuffleBufferBytes}, nil
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
	inputs := append([]Input(nil), source.inputs...)
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
		err := shard.WalkRecords(input.Path, func(_ int64, view shard.RecordView) error {
			record := Record{ID: view.ID, Text: view.Text, Source: view.Source, License: view.License, Language: view.Language}
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

// recordMemoryBytes accounts for retained string data and the five string
// headers in Record. It is deliberately conservative and keeps the shuffle
// window bounded by bytes as well as record count.
func recordMemoryBytes(record Record) int64 {
	return int64(5*16 + len(record.ID) + len(record.Text) + len(record.Source) + len(record.License) + len(record.Language))
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
