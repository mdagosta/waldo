// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package corpus resolves indexed manifests into immutable, verified selections
// consumed by export and model workflows.
package corpus

import (
	"fmt"
	"path"
)

type LicensePolicy struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

func NewLicensePolicy(include, exclude []string) (LicensePolicy, error) {
	policy := LicensePolicy{Include: compact(include), Exclude: compact(exclude)}
	for _, pattern := range append(append([]string{}, policy.Include...), policy.Exclude...) {
		if _, err := path.Match(pattern, "probe"); err != nil {
			return LicensePolicy{}, fmt.Errorf("invalid license pattern %q: %w", pattern, err)
		}
	}
	return policy, nil
}

func (p LicensePolicy) Allows(license string) bool {
	for _, pattern := range p.Exclude {
		if matches(pattern, license) {
			return false
		}
	}
	if len(p.Include) == 0 {
		return true
	}
	for _, pattern := range p.Include {
		if matches(pattern, license) {
			return true
		}
	}
	return false
}

func matches(pattern, value string) bool {
	matched, _ := path.Match(pattern, value)
	return matched
}

func compact(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
