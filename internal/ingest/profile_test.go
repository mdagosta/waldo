package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordProfilePlansDetectedContainers(t *testing.T) {
	for name, contents := range map[string]string{
		"one.json":   `{"body":"one"}`,
		"many.jsonl": "{\"body\":\"one\"}\n{\"body\":\"two\"}\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}
			probe, err := ProbePaths(context.Background(), []string{path})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewPlan(probe, PlanRequest{
				Destination: "core/profile", Title: "Profile", License: "CC0-1.0",
				Source:  PlanSource{Name: "profile", URL: "https://example.test", Category: "public-dataset"},
				Profile: InputProfile{Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"body"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Inputs[0].Adapter != probe.Artifacts[0].Format || plan.Inputs[0].Profile.Type != ProfileRecordMap {
				t.Fatalf("plan input = %+v", plan.Inputs[0])
			}
		})
	}
}

func TestInputProfileValidation(t *testing.T) {
	for name, profile := range map[string]InputProfile{
		"missing text":         {Type: ProfileRecordMap},
		"bad path":             {Type: ProfileRecordMap, Fields: ProfileFields{Text: []string{"body[0]"}}},
		"missing reply":        {Type: ProfileDialoguePair, Fields: ProfileFields{Text: []string{"prompt"}}},
		"incomplete tree":      {Type: ProfileRankedConversationTree, Tree: ConversationTree{Text: "text"}},
		"bad boundary":         {Type: ProfileBoundedText, Bounds: TextBounds{StartPattern: "[", EndPattern: "end"}},
		"relative XPath":       {Type: ProfileXMLRecord, Fields: ProfileFields{Text: []string{"doc/body"}}},
		"bad empty policy":     {Type: ProfileRecordMap, OnEmpty: "discard", Fields: ProfileFields{Text: []string{"body"}}},
		"empty policy on file": {Type: ProfileBoundedText, OnEmpty: "skip", Bounds: TextBounds{StartPattern: "start", EndPattern: "end"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := profile.Validate(); err == nil {
				t.Fatalf("profile was accepted: %+v", profile)
			}
		})
	}
}

func TestLoadInputProfileIsStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, []byte("type: record-map\nfields:\n  text: [body]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile, err := LoadInputProfile(path)
	if err != nil || profile.Type != ProfileRecordMap {
		t.Fatalf("profile = %+v, error = %v", profile, err)
	}
	if err := os.WriteFile(path, []byte("type: record-map\nunknown: true\nfields:\n  text: [body]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInputProfile(path); err == nil {
		t.Fatal("profile with unknown field was accepted")
	}
}
