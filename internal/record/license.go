package record

import (
	"regexp"
	"strings"
)

var creativeCommonsURL = regexp.MustCompile(`(?i)^https?://creativecommons\.org/(?:licenses/([a-z-]+)/([0-9.]+)|publicdomain/zero/([0-9.]+))/?(?:legalcode/?)?(?:[?#].*)?$`)
var creativeCommonsName = regexp.MustCompile(`(?i)^CC[ _-]*(BY(?:[ _-]*(?:NC|ND|SA)){0,2}|ZERO|0)[ _-]*([0-9]+(?:\.[0-9]+)?)$`)

// NormalizeLicense canonicalizes recognized Creative Commons identifiers.
// Unknown expressions are preserved verbatim after surrounding whitespace is
// removed; WALDO does not invent an identity for upstream license evidence.
func NormalizeLicense(raw string) string {
	value := strings.TrimSpace(raw)
	if match := creativeCommonsURL.FindStringSubmatch(value); match != nil {
		if match[3] != "" {
			return "CC0-" + match[3]
		}
		return "CC-" + strings.ToUpper(match[1]) + "-" + match[2]
	}
	if match := creativeCommonsName.FindStringSubmatch(value); match != nil {
		name := strings.ToUpper(strings.NewReplacer(" ", "-", "_", "-").Replace(match[1]))
		name = strings.ReplaceAll(name, "ZERO", "0")
		if name == "0" {
			return "CC0-" + match[2]
		}
		return "CC-" + name + "-" + match[2]
	}
	return value
}
