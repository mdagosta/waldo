// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"fmt"

	waldoTokenizer "github.com/openwaldo/waldo/internal/tokenizer"
)

const (
	ByteTokenizerRevision    = "builtin-byte-schema-1"
	TiktokenCL100KRevision   = "tiktoken-cl100k-base"
	TiktokenCL100KVocabulary = 100259
	TiktokenCL100KPadID      = 100256
	TiktokenCL100KBOSID      = 100257
	TiktokenCL100KEOSID      = 100258
)

type TokenizerSpec struct {
	Name           string `json:"name"`
	Revision       string `json:"revision"`
	VocabularySize int    `json:"vocabulary_size"`
	PadID          int    `json:"pad_id"`
	BOSID          int    `json:"bos_id"`
	EOSID          int    `json:"eos_id"`
}

type TokenCodec interface {
	Count(string) int
	Encode(string) []int
	Decode([]int) string
}

type byteCodec struct{}

func (byteCodec) Count(text string) int { return len([]byte(text)) }
func (byteCodec) Encode(text string) []int {
	encoded := make([]int, len([]byte(text)))
	for index, value := range []byte(text) {
		encoded[index] = int(value) + 3
	}
	return encoded
}
func (byteCodec) Decode(tokens []int) string {
	decoded := make([]byte, 0, len(tokens))
	for _, token := range tokens {
		if token >= 3 && token <= 258 {
			decoded = append(decoded, byte(token-3))
		}
	}
	return string(decoded)
}

func ResolveTokenizer(name, revision string, vocabularySize uint64) (TokenizerSpec, TokenCodec, error) {
	switch {
	case name == "byte" && revision == ByteTokenizerRevision && vocabularySize == 259:
		return TokenizerSpec{Name: name, Revision: revision, VocabularySize: 259, PadID: 0, BOSID: 1, EOSID: 2}, byteCodec{}, nil
	case name == waldoTokenizer.Default && revision == TiktokenCL100KRevision && vocabularySize == TiktokenCL100KVocabulary:
		codec, err := waldoTokenizer.NewCodec(name)
		if err != nil {
			return TokenizerSpec{}, nil, err
		}
		return TokenizerSpec{Name: name, Revision: revision, VocabularySize: TiktokenCL100KVocabulary, PadID: TiktokenCL100KPadID, BOSID: TiktokenCL100KBOSID, EOSID: TiktokenCL100KEOSID}, codec, nil
	default:
		return TokenizerSpec{}, nil, fmt.Errorf("unsupported tokenizer %s@%s with vocabulary_size %d", name, revision, vocabularySize)
	}
}

// ResolveTokenizerCodec resolves tokenization semantics before backend
// selection. Legacy test and imported architectures may carry opaque byte
// tokenizer revisions; real backends still use ResolveTokenizer to fail closed
// on the exact executable artifact contract.
func ResolveTokenizerCodec(name string) (TokenCodec, error) {
	if name == "byte" {
		return byteCodec{}, nil
	}
	if name == waldoTokenizer.Default {
		return waldoTokenizer.NewCodec(name)
	}
	return nil, fmt.Errorf("unsupported tokenizer %q", name)
}
