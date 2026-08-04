package record

import "testing"

func TestAppendCanonical(t *testing.T) {
	text := "A <record> — with\ncontrols."
	record := Record{
		SHA256: TextHash(text), Kind: KindPretrain, Text: text,
		Source: "https://example.test/a", SourceName: "fixture",
		License: "CC0-1.0", Lang: "en", LangScore: 991, Tokens: 7,
	}
	got, err := record.AppendCanonical(nil, []byte(`{"a":1,"b":"two"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sha256":"` + TextHash(text) + `","kind":"pretrain","text":"A <record> — with\ncontrols.","source":"https://example.test/a","source_name":"fixture","license":"CC0-1.0","lang":"en","lang_score":991,"tokens":7,"meta":{"a":1,"b":"two"}}` + "\n"
	if string(got) != want {
		t.Fatalf("canonical record:\n got %q\nwant %q", got, want)
	}
}

func TestAppendCanonicalRejectsTextHashMismatch(t *testing.T) {
	record := Record{SHA256: TextHash("other"), Kind: KindPretrain, Text: "text", Source: "source", License: "CC0-1.0"}
	if _, err := record.AppendCanonical(nil, nil); err == nil {
		t.Fatal("expected text hash mismatch")
	}
}
