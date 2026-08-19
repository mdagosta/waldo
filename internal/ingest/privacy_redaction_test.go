// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"strings"
	"testing"

	"github.com/openwaldo/waldo/internal/record"
)

func TestPrivacyRedactionRemovesDirectIdentifiersAndRetainsNames(t *testing.T) {
	input := "From: Jane Doe <jane@example.org>\nReceived: from relay.example (192.0.2.4)\nMessage-ID: <x@example.org>\nSubject: Call me\n\nJane Doe can be reached at +1 (415) 555-0123 from 2001:db8::1. password=hunter2-secret-value"
	got, redaction, err := redactPrivacy(input)
	if err != nil {
		t.Fatal(err)
	}
	if redaction.EmailAddresses != 1 || redaction.IPAddresses != 1 || redaction.PhoneNumbers != 1 || redaction.MailRoutingHeaders != 2 || redaction.Credentials != 1 {
		t.Fatalf("counts = %s", redaction.counts())
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
	got, redaction, err := redactPrivacy(input)
	if err != nil {
		t.Fatal(err)
	}
	if got != input || !redaction.empty() {
		t.Fatalf("ordinary content changed (%s)", redaction.counts())
	}
}

func TestPrivacyRedactionRemovesPrivateKeyBlock(t *testing.T) {
	input := "before\n-----BEGIN PRIVATE KEY-----\nsecret material\n-----END PRIVATE KEY-----\nafter"
	got, redaction, err := redactPrivacy(input)
	if err != nil {
		t.Fatal(err)
	}
	if redaction.Credentials != 1 || strings.Contains(got, "secret material") || !strings.Contains(got, credentialPlaceholder) {
		t.Fatalf("private key redaction count = %d", redaction.Credentials)
	}
}

func TestPrivacyRedactionConvergesAfterPhoneExposesIPv6Candidate(t *testing.T) {
	input := "reserved examples 192.0.2.10 and 198.51.100.20; overlap 2001:db8::7.415-555-0123"
	firstText, first := redactPrivacyPass(input)
	if first.IPAddresses != 2 || first.PhoneNumbers != 1 || first.EmailAddresses+first.MailRoutingHeaders+first.Credentials != 0 {
		t.Fatalf("first-pass counts = %s", first.counts())
	}
	_, second := redactPrivacyPass(firstText)
	if second.IPAddresses != 1 || second.EmailAddresses+second.PhoneNumbers+second.MailRoutingHeaders+second.Credentials != 0 {
		t.Fatalf("second-pass counts = %s", second.counts())
	}

	redacted, total, err := redactPrivacy(input)
	if err != nil {
		t.Fatal(err)
	}
	if total.IPAddresses != 3 || total.PhoneNumbers != 1 || total.EmailAddresses+total.MailRoutingHeaders+total.Credentials != 0 {
		t.Fatalf("cumulative counts = %s", total.counts())
	}
	redactedAgain, remaining, err := redactPrivacy(redacted)
	if err != nil {
		t.Fatal(err)
	}
	if redactedAgain != redacted || !remaining.empty() {
		t.Fatalf("fixed-point counts = %s", remaining.counts())
	}
}

func TestPrivacyRedactionIsGenerallyIdempotent(t *testing.T) {
	inputs := []string{
		"ordinary prose without identifiers",
		"Contact test@example.org or +1 (202) 555-0142 from 203.0.113.8.",
		"IPv6 documentation address 2001:db8:1::9 and api_key=abcdefghijklmnop.",
	}
	for position, input := range inputs {
		redacted, _, err := redactPrivacy(input)
		if err != nil {
			t.Fatalf("case %d first pass: %v", position, err)
		}
		redactedAgain, remaining, err := redactPrivacy(redacted)
		if err != nil {
			t.Fatalf("case %d second pass: %v", position, err)
		}
		if redactedAgain != redacted || !remaining.empty() {
			t.Fatalf("case %d second-pass counts = %s", position, remaining.counts())
		}
	}
}

func TestConversationPrivacyInvariantKeepsMessageBoundaries(t *testing.T) {
	payload, err := record.EncodeConversation(record.Conversation{Messages: []record.Message{
		{Role: "user", Content: "Received: this is ordinary message content"},
		{Role: "assistant", Content: "Subject: response\n\nNothing private here."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	redacted, first, err := redactCanonicalPayload(record.KindConversation, payload)
	if err != nil {
		t.Fatal(err)
	}
	_, remaining, err := redactCanonicalPayload(record.KindConversation, redacted)
	if err != nil {
		t.Fatal(err)
	}
	if redacted != payload || !first.empty() || !remaining.empty() {
		t.Fatalf("conversation redaction changed ordinary messages: first=%s remaining=%s", first.counts(), remaining.counts())
	}
	_, joined := redactPrivacyPass("Received: this is ordinary message content\nSubject: response\n\nNothing private here.")
	if joined.MailRoutingHeaders != 1 {
		t.Fatalf("fixture did not reproduce joined-message false positive: %s", joined.counts())
	}
}

func TestPrivacyRedactionFailsClosedAtDeterministicPassLimit(t *testing.T) {
	_, total, err := convergePrivacyRedaction("synthetic", func(value string) (string, privacyRedaction) {
		return value + "x", privacyRedaction{IPAddresses: 1}
	})
	if err == nil || total.IPAddresses != privacyRedactionPassLimit || !strings.Contains(err.Error(), "after 8 passes") || !strings.Contains(err.Error(), "email=0, ip=1, phone=0, routing=0, credential=0") {
		t.Fatalf("non-convergence error = %v", err)
	}
}

func TestPrivacyPlaceholdersNeverMatchDetectors(t *testing.T) {
	if privacyPlaceholderInvariant != nil {
		t.Fatal(privacyPlaceholderInvariant)
	}
	for _, placeholder := range []string{emailPlaceholder, ipPlaceholder, phonePlaceholder, credentialPlaceholder} {
		redacted, matches := redactPrivacyPass(placeholder)
		if redacted != placeholder || !matches.empty() {
			t.Fatalf("placeholder detector counts = %s", matches.counts())
		}
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
