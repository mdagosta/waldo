// Copyright (c) 2026 OpenWALDO Project contributors
// SPDX-License-Identifier: Apache-2.0

package index

import "testing"

func TestDeclaredLanguagesUsesCorpusDeclarationAndSourceFallback(t *testing.T) {
	manifest := Manifest{Sources: []Source{{Content: &Content{Languages: []string{"es"}, ProgrammingLanguages: []string{"Python"}}}}}
	human, programming := DeclaredLanguages(manifest)
	if len(human) != 1 || human[0] != "es" || len(programming) != 1 || programming[0] != "Python" {
		t.Fatalf("source fallback = %v / %v", human, programming)
	}
	manifest.Content = &Content{Languages: []string{"en"}, ProgrammingLanguages: []string{"Go"}}
	human, programming = DeclaredLanguages(manifest)
	if len(human) != 1 || human[0] != "en" || len(programming) != 1 || programming[0] != "Go" {
		t.Fatalf("corpus declaration = %v / %v", human, programming)
	}
}
