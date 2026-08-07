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
