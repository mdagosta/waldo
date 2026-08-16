// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"strings"
	"testing"
)

func TestPrivacyRedactionRemovesDirectIdentifiersAndRetainsNames(t *testing.T) {
	input := "From: Jane Doe <jane@example.org>\nReceived: from relay.example (192.0.2.4)\nMessage-ID: <x@example.org>\nSubject: Call me\n\nJane Doe can be reached at +1 (415) 555-0123 from 2001:db8::1. password=hunter2-secret-value"
	got, emails, ips, phones, routing, credentials := redactPrivacy(input)
	if emails != 1 || ips != 1 || phones != 1 || routing != 2 || credentials != 1 {
		t.Fatalf("counts = email %d ip %d phone %d routing %d credentials %d\n%s", emails, ips, phones, routing, credentials, got)
	}
	for _, private := range []string{"jane@example.org", "192.0.2.4", "2001:db8::1", "415", "hunter2-secret-value", "Received:", "Message-ID:"} {
		if strings.Contains(got, private) {
			t.Fatalf("redacted text retained %q: %s", private, got)
		}
	}
	if !strings.Contains(got, "Jane Doe") || !strings.Contains(got, emailPlaceholder) || !strings.Contains(got, ipPlaceholder) || !strings.Contains(got, phonePlaceholder) || !strings.Contains(got, credentialPlaceholder) {
		t.Fatalf("redacted text did not retain attribution/placeholders: %s", got)
	}
}

func TestPrivacyRedactionAvoidsBareNumbersAndProseHeaders(t *testing.T) {
	input := "Received: this is a prose label, not an RFC 822 header\nLinux 6.12 has commit 123456789 and version 1.2.3."
	got, emails, ips, phones, routing, credentials := redactPrivacy(input)
	if got != input || emails+ips+phones+routing+credentials != 0 {
		t.Fatalf("ordinary content changed: %q (%d/%d/%d/%d/%d)", got, emails, ips, phones, routing, credentials)
	}
}

func TestPrivacyRedactionRemovesPrivateKeyBlock(t *testing.T) {
	input := "before\n-----BEGIN PRIVATE KEY-----\nsecret material\n-----END PRIVATE KEY-----\nafter"
	got, _, _, _, _, credentials := redactPrivacy(input)
	if credentials != 1 || strings.Contains(got, "secret material") || !strings.Contains(got, credentialPlaceholder) {
		t.Fatalf("private key redaction = %q, %d", got, credentials)
	}
}

func TestRedactionPrecedesCanonicalDeduplication(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, directory+"/one.txt", "Contact first@example.org")
	writeFixture(t, directory+"/two.txt", "Contact second@example.org")
	probe, err := ProbePaths(t.Context(), []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(probe, PlanRequest{Destination: "core/privacy", Title: "Privacy", Description: "Privacy fixture.", License: "CC0-1.0", Source: PlanSource{Name: "fixture", URL: "https://example.test", Category: "public-dataset"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := AssembleTextObjects(t.Context(), plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.InputDocs != 2 || result.RetainedDocs != 1 || result.DuplicateDocs != 1 || len(result.Objects) != 1 || result.Objects[0].Redaction.EmailAddresses != 1 {
		t.Fatalf("redacted dedup result = %+v", result)
	}
}
