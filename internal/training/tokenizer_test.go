// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"context"
	"testing"
)

func TestCL100KTokenizerRoundTripAndSpecialFraming(t *testing.T) {
	spec, codec, err := ResolveTokenizer("tiktoken/cl100k_base", TiktokenCL100KRevision, TiktokenCL100KVocabulary)
	if err != nil {
		t.Fatal(err)
	}
	text := "France: Paris — 日本"
	tokens := codec.Encode(text)
	if len(tokens) == 0 || codec.Decode(tokens) != text {
		t.Fatalf("round trip = %q through %v", codec.Decode(tokens), tokens)
	}
	if spec.PadID != 100256 || spec.BOSID != 100257 || spec.EOSID != 100258 || spec.VocabularySize != 100259 {
		t.Fatalf("special framing = %+v", spec)
	}
	for _, token := range tokens {
		if token >= spec.PadID {
			t.Fatalf("ordinary token %d overlaps special token range", token)
		}
	}
}

func TestTokenizedRecordSourceRemovesRawText(t *testing.T) {
	_, codec, err := ResolveTokenizer("tiktoken/cl100k_base", TiktokenCL100KRevision, TiktokenCL100KVocabulary)
	if err != nil {
		t.Fatal(err)
	}
	source := tokenizedRecordSource{source: staticRecordSource{{Text: "hello", Corpus: "example"}}, codec: codec}
	if err := source.Stream(t.Context(), func(record Record) error {
		if record.Text != "" || len(record.Tokens) == 0 || record.Corpus != "example" {
			t.Fatalf("tokenized record = %+v", record)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type staticRecordSource []Record

func (source staticRecordSource) Stream(ctx context.Context, consume func(Record) error) error {
	for _, record := range source {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := consume(record); err != nil {
			return err
		}
	}
	return nil
}
