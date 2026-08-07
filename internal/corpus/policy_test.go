// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package corpus

import "testing"

func TestLicensePolicy(t *testing.T) {
	policy, err := NewLicensePolicy([]string{"CC-*", "Apache-2.0"}, []string{"CC-BY-NC-*"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		license string
		want    bool
	}{
		{"CC-BY-4.0", true},
		{"Apache-2.0", true},
		{"CC-BY-NC-4.0", false},
		{"MIT", false},
	}
	for _, test := range tests {
		if got := policy.Allows(test.license); got != test.want {
			t.Errorf("Allows(%q) = %v, want %v", test.license, got, test.want)
		}
	}
}

func TestLicensePolicyRejectsInvalidGlob(t *testing.T) {
	if _, err := NewLicensePolicy([]string{"["}, nil); err == nil {
		t.Fatal("NewLicensePolicy() accepted invalid glob")
	}
}
