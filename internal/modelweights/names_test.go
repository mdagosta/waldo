package modelweights

import "testing"

func TestTensorNamesRoundTrip(t *testing.T) {
	for _, name := range []string{"embedding.weight", "norm.weight", "output.weight", "layers.12.attention.q_proj.weight", "layers.3.feed_forward.down.weight"} {
		huggingFace, err := HuggingFaceName(name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := WALDOName(huggingFace)
		if err != nil {
			t.Fatal(err)
		}
		if got != name {
			t.Fatalf("%s -> %s -> %s", name, huggingFace, got)
		}
	}
}
