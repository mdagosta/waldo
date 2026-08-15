// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import "testing"

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
