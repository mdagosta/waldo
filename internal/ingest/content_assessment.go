// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"regexp"
	"strings"
	"unicode"
)

// emailAddressPattern deliberately detects common Internet email-shaped
// strings. It is a content flag, not a claim that the value identifies a
// natural person or that every RFC 5322 address is recognized.
var emailAddressPattern = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`)

func containsEmailAddress(text string) bool {
	return emailAddressPattern.MatchString(text)
}

type rowAssessment struct {
	EmailAddresses     bool
	RepetitiveContent  bool
	BoilerplateContent bool
}

func assessContent(text string) rowAssessment {
	boilerplate, repetitive := assessRepetition(text)
	return rowAssessment{
		EmailAddresses:     containsEmailAddress(text),
		RepetitiveContent:  repetitive,
		BoilerplateContent: boilerplate,
	}
}

// assessRepetition is a language-neutral, deterministic adaptation of the
// within-document repetition checks published with Gopher. Structural
// duplicate-line/paragraph checks identify boilerplate; repeated token n-grams
// identify repetitive content. The exact contract is pinned by the detector
// identities recorded with schema-2 shards.
func assessRepetition(text string) (boilerplate, repetitive bool) {
	if len(text) == 0 {
		return false, false
	}
	lines := splitNonempty(text, "\n")
	paragraphs := splitParagraphs(text)
	boilerplate = duplicateStructure(lines, len(text), 0.30, 0.20) || duplicateStructure(paragraphs, len(text), 0.30, 0.20)

	words := assessmentWords(text)
	if len(words) < 50 {
		return boilerplate, false
	}
	return boilerplate, topNGramFraction(words, 3) > 0.18 || duplicateNGramFraction(words, 8) > 0.12
}

func splitNonempty(text, separator string) []string {
	parts := strings.Split(text, separator)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Join(strings.Fields(part), " ")
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

var paragraphPattern = regexp.MustCompile(`\n[ \t\r]*\n+`)

func splitParagraphs(text string) []string {
	parts := paragraphPattern.Split(strings.TrimSpace(text), -1)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Join(strings.Fields(part), " ")
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func duplicateStructure(values []string, textBytes int, elementThreshold, byteThreshold float64) bool {
	if len(values) < 4 || textBytes == 0 {
		return false
	}
	seen := make(map[string]bool, len(values))
	duplicates, duplicateBytes := 0, 0
	for _, value := range values {
		if seen[value] {
			duplicates++
			duplicateBytes += len(value)
		} else {
			seen[value] = true
		}
	}
	return float64(duplicates)/float64(len(values)) > elementThreshold || float64(duplicateBytes)/float64(textBytes) > byteThreshold
}

func assessmentWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(value rune) bool {
		return !unicode.IsLetter(value) && !unicode.IsNumber(value)
	})
}

func ngramKey(words []string) string {
	return strings.Join(words, "\x1f")
}

func topNGramFraction(words []string, width int) float64 {
	if len(words) < width {
		return 0
	}
	counts := make(map[string]int, len(words)-width+1)
	top := 0
	for position := 0; position <= len(words)-width; position++ {
		key := ngramKey(words[position : position+width])
		counts[key]++
		if counts[key] > top {
			top = counts[key]
		}
	}
	return float64(top*width) / float64(len(words))
}

func duplicateNGramFraction(words []string, width int) float64 {
	if len(words) < width {
		return 0
	}
	seen := make(map[string]bool, len(words)-width+1)
	duplicateWords := 0
	for position := 0; position <= len(words)-width; {
		key := ngramKey(words[position : position+width])
		if seen[key] {
			duplicateWords += width
			position += width
		} else {
			seen[key] = true
			position++
		}
	}
	return float64(duplicateWords) / float64(len(words))
}
