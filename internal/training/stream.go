package training

import (
	"context"
	"fmt"
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

type canonicalRecordSource struct {
	inputs      []Input
	seed        uint64
	buffer      int
	bufferBytes int64
}

func NewCanonicalRecordSource(inputs []Input, parameters ResolvedParameters) (RecordSource, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("canonical record source requires at least one shard input")
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
	random := newSplitMix64(parameters.Seed)
	for index := len(ordered) - 1; index > 0; index-- {
		other := random.intn(index + 1)
		ordered[index], ordered[other] = ordered[other], ordered[index]
	}
	return &canonicalRecordSource{inputs: ordered, seed: parameters.Seed, buffer: parameters.Data.ShuffleBufferRecords, bufferBytes: parameters.Data.ShuffleBufferBytes}, nil
}

func (source *canonicalRecordSource) Stream(ctx context.Context, consume func(Record) error) error {
	if consume == nil {
		return fmt.Errorf("record consumer is required")
	}
	random := newSplitMix64(source.seed ^ 0x6a09e667f3bcc909)
	buffer := make([]Record, 0, source.buffer)
	var bufferedBytes int64
	emit := func(record Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return consume(record)
	}
	for _, input := range source.inputs {
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
			return fmt.Errorf("stream canonical shard %s: %w", input.SHA256, err)
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
