// Copyright (c) 2026 OpenWALDO Project contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	waldoindex "github.com/openwaldo/waldo/internal/index"
)

func TestFilterCorporaByDeclaredLanguageExcludesUnknown(t *testing.T) {
	corpora := []waldoindex.CorpusInfo{
		{Path: "english", Languages: []string{"en"}},
		{Path: "spanish-python", Languages: []string{"es"}, ProgrammingLanguages: []string{"Python"}},
		{Path: "unknown"},
	}
	filtered := filterCorporaByLanguage(corpora, []string{"ES"}, []string{"python"})
	if len(filtered) != 1 || filtered[0].Path != "spanish-python" {
		t.Fatalf("filtered corpora = %+v", filtered)
	}
	if unfiltered := filterCorporaByLanguage(corpora, nil, nil); len(unfiltered) != 3 {
		t.Fatalf("unfiltered corpora = %+v", unfiltered)
	}
}
