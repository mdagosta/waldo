// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package tokenizer

import "testing"

func TestDefaultCounterIsOfflineDeterministicAndCached(t *testing.T) {
	first, err := Get(Default)
	if err != nil {
		t.Fatal(err)
	}
	if first.Name() != Default {
		t.Fatalf("name = %q", first.Name())
	}
	text := "Call me Ishmael. Some years ago—never mind how long precisely."
	if count := first.Count(text); count < 10 || count > 30 {
		t.Fatalf("implausible token count %d", count)
	}
	second, err := Get(Default)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.Count(text) != second.Count(text) {
		t.Fatal("default counter was not cached or deterministic")
	}
	if _, err := Get("made-up/nope"); err == nil {
		t.Fatal("unknown tokenizer accepted")
	}
}

func TestNewReturnsIndependentEquivalentCounters(t *testing.T) {
	first, err := New(Default)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(Default)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("New returned the same counter instance")
	}
	if first.Count("parallel audit") != second.Count("parallel audit") {
		t.Fatal("independent counters disagree")
	}
}

func TestR50KCodecIsAvailableOffline(t *testing.T) {
	codec, err := NewCodec("tiktoken/r50k_base")
	if err != nil {
		t.Fatal(err)
	}
	text := "portable GPT-2 vocabulary"
	if tokens := codec.Encode(text); len(tokens) == 0 || codec.Decode(tokens) != text {
		t.Fatalf("r50k round trip failed through %v", tokens)
	}
}
