package model

import "testing"

func TestRecommendedPresetUsesTwentyTokensPerParameter(t *testing.T) {
	small, err := RecommendedPreset(1)
	if err != nil {
		t.Fatal(err)
	}
	if small.Name != "10m" {
		t.Fatalf("small preset = %s", small.Name)
	}
	first, err := small.Architecture.Forecast()
	if err != nil {
		t.Fatal(err)
	}
	large, err := RecommendedPreset(int64(first.ApproximateParameters * 20))
	if err != nil {
		t.Fatal(err)
	}
	if large.Name != "10m" {
		t.Fatalf("threshold preset = %s", large.Name)
	}
}

func TestForecastIndexSelectionUsesOnePass(t *testing.T) {
	_, report, err := ForecastIndexSelection(123_456_789)
	if err != nil {
		t.Fatal(err)
	}
	if report.PlannedTokens != 123_456_789 || len(report.Configurations) == 0 {
		t.Fatalf("report = %+v", report)
	}
}
