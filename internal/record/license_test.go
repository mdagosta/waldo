// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package record

import "testing"

func TestNormalizeLicense(t *testing.T) {
	for input, wanted := range map[string]string{
		"https://creativecommons.org/licenses/by/4.0/":       "CC-BY-4.0",
		"http://creativecommons.org/licenses/by-nc-sa/3.0":   "CC-BY-NC-SA-3.0",
		"https://creativecommons.org/publicdomain/zero/1.0/": "CC0-1.0",
		"CC BY 4.0":           "CC-BY-4.0",
		"LicenseRef-Upstream": "LicenseRef-Upstream",
	} {
		if got := NormalizeLicense(input); got != wanted {
			t.Errorf("NormalizeLicense(%q) = %q, want %q", input, got, wanted)
		}
	}
}
