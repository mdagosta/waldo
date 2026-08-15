// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"fmt"
	"strings"
	"testing"
)

func TestContainsEmailAddress(t *testing.T) {
	for name, test := range map[string]struct {
		text string
		want bool
	}{
		"plain":        {text: "Contact maintainer@example.org.", want: true},
		"tagged":       {text: "Author: <first.last+code@example.co.uk>", want: true},
		"ordinary":     {text: "No contact information is present.", want: false},
		"at sign":      {text: "Use @decorator in this example.", want: false},
		"local domain": {text: "user@localhost is not a public Internet address.", want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := containsEmailAddress(test.text); got != test.want {
				t.Fatalf("containsEmailAddress(%q) = %t, want %t", test.text, got, test.want)
			}
		})
	}
}

func TestAssessContentUsesGenericStructuralAndNGramRules(t *testing.T) {
	cleanWords := make([]string, 80)
	for position := range cleanWords {
		cleanWords[position] = fmt.Sprintf("word%d", position)
	}
	clean := strings.Join(cleanWords, " ")
	if got := assessContent(clean); got.RepetitiveContent || got.BoilerplateContent {
		t.Fatalf("clean assessment = %+v", got)
	}

	line := "Navigation links and repeated site furniture"
	boilerplate := strings.Join([]string{line, "Useful article text one", line, line, "Useful article text two"}, "\n")
	if got := assessContent(boilerplate); !got.BoilerplateContent {
		t.Fatalf("boilerplate assessment = %+v", got)
	}

	phrase := "alpha beta gamma delta epsilon zeta eta theta "
	repetitive := strings.Repeat(phrase, 10)
	if got := assessContent(repetitive); !got.RepetitiveContent {
		t.Fatalf("repetitive assessment = %+v", got)
	}
}
