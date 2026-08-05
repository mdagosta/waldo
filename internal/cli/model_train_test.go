package cli

import "testing"

func TestParseModelTrainDefaultsAndValidatesEpochs(t *testing.T) {
	name, paths, epochs, err := parseModelTrain([]string{"foo", "core/books", "science/papers"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "foo" || epochs != 1 || len(paths) != 2 {
		t.Fatalf("name = %q, paths = %v, epochs = %d", name, paths, epochs)
	}
	_, _, epochs, err = parseModelTrain([]string{"foo", "core/books", "--epochs", "3"})
	if err != nil || epochs != 3 {
		t.Fatalf("epochs = %d, error = %v", epochs, err)
	}
	if _, _, _, err := parseModelTrain([]string{"foo", "core/books", "--epochs", "0"}); err == nil {
		t.Fatal("zero epochs accepted")
	}
}
