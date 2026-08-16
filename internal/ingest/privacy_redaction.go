// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/openwaldo/waldo/internal/shard"
)

const (
	emailPlaceholder          = "<EMAIL_ADDRESS>"
	ipPlaceholder             = "<IP_ADDRESS>"
	phonePlaceholder          = "<PHONE_NUMBER>"
	credentialPlaceholder     = "<CREDENTIAL>"
	privacyRedactionPassLimit = 8
)

type privacyRedaction struct {
	EmailAddresses     int64
	IPAddresses        int64
	PhoneNumbers       int64
	MailRoutingHeaders int64
	Credentials        int64
}

func (value privacyRedaction) empty() bool {
	return value.EmailAddresses == 0 && value.IPAddresses == 0 && value.PhoneNumbers == 0 && value.MailRoutingHeaders == 0 && value.Credentials == 0
}

func (value privacyRedaction) counts() string {
	return fmt.Sprintf("email=%d, ip=%d, phone=%d, routing=%d, credential=%d", value.EmailAddresses, value.IPAddresses, value.PhoneNumbers, value.MailRoutingHeaders, value.Credentials)
}

var (
	privateKeyPattern     = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----.*?-----END (?:[A-Z0-9 ]+ )?PRIVATE KEY-----`)
	credentialPattern     = regexp.MustCompile(`(?im)(authorization\s*:\s*(?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]+|((?:password|passwd|api[_-]?key|access[_-]?token|secret)\s*[:=]\s*)(?:"[^"\r\n]{8,}"|'[^'\r\n]{8,}'|[A-Za-z0-9_./+=-]{16,})|\b(?:AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,})\b`)
	ipCandidatePattern    = regexp.MustCompile(`\[?[0-9A-Fa-f:.]*[0-9A-Fa-f][0-9A-Fa-f:.]*\]?`)
	phoneCandidatePattern = regexp.MustCompile(`(?:\+[0-9]{1,3}[ .-]?)?(?:\([0-9]{2,4}\)[ .-]?|[0-9]{2,4}[ .-])[0-9]{2,4}[ .-][0-9]{3,4}`)
)

var mailRoutingHeaders = map[string]bool{
	"received": true, "return-path": true, "x-originating-ip": true,
	"authentication-results": true, "arc-authentication-results": true,
	"arc-message-signature": true, "arc-seal": true, "dkim-signature": true,
	"received-spf": true, "x-received": true, "delivered-to": true,
	"message-id": true,
}

var privacyPlaceholderInvariant = validatePrivacyPlaceholders()

// redactCanonicalBatch applies the mandatory schema-2 privacy policy before
// content identity, deduplication, token measurement, or Parquet encoding.
func redactCanonicalBatch(batch TextBatch) (TextBatch, error) {
	batch.LogicalBytes = 0
	for position := range batch.Rows {
		row := &batch.Rows[position]
		redactedText, redaction, err := redactPrivacy(row.Text)
		if err != nil {
			return TextBatch{}, fmt.Errorf("redact canonical text: %w", err)
		}
		row.Text = redactedText
		addRowRedaction(row, redaction)
		redactedSource, redaction, err := redactString(row.Source)
		if err != nil {
			return TextBatch{}, fmt.Errorf("redact canonical source: %w", err)
		}
		row.Source = redactedSource
		addRowRedaction(row, redaction)
		if row.Meta != nil {
			redacted, redaction, err := redactJSONStrings(*row.Meta)
			if err != nil {
				return TextBatch{}, fmt.Errorf("redact canonical metadata: %w", err)
			}
			row.Meta = &redacted
			addRowRedaction(row, redaction)
		}
		digest := sha256.Sum256([]byte(row.Text))
		row.ContentSHA256 = digest
		batch.LogicalBytes += int64(len(row.Text))
	}
	return batch, nil
}

func redactString(text string) (string, privacyRedaction, error) {
	return redactPrivacy(text)
}

func addRowRedaction(row *shard.TextRow, value privacyRedaction) {
	row.RedactedEmailAddresses += value.EmailAddresses
	row.RedactedIPAddresses += value.IPAddresses
	row.RedactedPhoneNumbers += value.PhoneNumbers
	row.RemovedMailRoutingHeaders += value.MailRoutingHeaders
	row.RedactedCredentials += value.Credentials
}

func redactJSONStrings(encoded string) (string, privacyRedaction, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", privacyRedaction{}, err
	}
	var total privacyRedaction
	if err := redactJSONValue(&value, &total); err != nil {
		return "", privacyRedaction{}, err
	}
	data, err := json.Marshal(value)
	return string(data), total, err
}

func redactJSONValue(value *any, total *privacyRedaction) error {
	switch typed := (*value).(type) {
	case string:
		redacted, one, err := redactString(typed)
		if err != nil {
			return err
		}
		*value = redacted
		addPrivacyRedaction(total, one)
	case []any:
		for position := range typed {
			if err := redactJSONValue(&typed[position], total); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range typed {
			if err := redactJSONValue(&child, total); err != nil {
				return err
			}
			typed[key] = child
		}
	}
	return nil
}

func addPrivacyRedaction(total *privacyRedaction, value privacyRedaction) {
	total.EmailAddresses += value.EmailAddresses
	total.IPAddresses += value.IPAddresses
	total.PhoneNumbers += value.PhoneNumbers
	total.MailRoutingHeaders += value.MailRoutingHeaders
	total.Credentials += value.Credentials
}

func redactPrivacy(text string) (string, privacyRedaction, error) {
	if privacyPlaceholderInvariant != nil {
		return "", privacyRedaction{}, privacyPlaceholderInvariant
	}
	return convergePrivacyRedaction(text, redactPrivacyPass)
}

func convergePrivacyRedaction(text string, pass func(string) (string, privacyRedaction)) (string, privacyRedaction, error) {
	var total privacyRedaction
	for passNumber := 1; passNumber <= privacyRedactionPassLimit; passNumber++ {
		redacted, current := pass(text)
		if current.empty() {
			return redacted, total, nil
		}
		addPrivacyRedaction(&total, current)
		text = redacted
	}
	_, remaining := pass(text)
	if !remaining.empty() {
		return text, total, fmt.Errorf("privacy redaction did not converge after %d passes; remaining detector counts: %s", privacyRedactionPassLimit, remaining.counts())
	}
	return text, total, nil
}

func redactPrivacyPass(text string) (string, privacyRedaction) {
	text, routing := removeMailRoutingHeaders(text)
	text, credentials := replaceMatches(text, privateKeyPattern, credentialPlaceholder)
	text, credentialValues := replaceMatches(text, credentialPattern, credentialPlaceholder)
	credentials += credentialValues
	text, emails := replaceMatches(text, emailAddressPattern, emailPlaceholder)
	text, ips := replaceValidated(text, ipCandidatePattern, ipPlaceholder, func(value string) bool {
		value = strings.Trim(value, "[].,;()")
		return strings.ContainsAny(value, ".:") && net.ParseIP(value) != nil
	})
	text, phones := replaceValidated(text, phoneCandidatePattern, phonePlaceholder, likelyPhoneNumber)
	return text, privacyRedaction{EmailAddresses: emails, IPAddresses: ips, PhoneNumbers: phones, MailRoutingHeaders: routing, Credentials: credentials}
}

func validatePrivacyPlaceholders() error {
	for _, placeholder := range []string{emailPlaceholder, ipPlaceholder, phonePlaceholder, credentialPlaceholder} {
		redacted, matches := redactPrivacyPass(placeholder)
		if redacted != placeholder || !matches.empty() {
			return fmt.Errorf("privacy placeholder matches a detector (%s)", matches.counts())
		}
	}
	return nil
}

func replaceMatches(text string, pattern *regexp.Regexp, replacement string) (string, int64) {
	var count int64
	return pattern.ReplaceAllStringFunc(text, func(string) string {
		count++
		return replacement
	}), count
}

func replaceValidated(text string, pattern *regexp.Regexp, replacement string, valid func(string) bool) (string, int64) {
	var count int64
	return pattern.ReplaceAllStringFunc(text, func(value string) string {
		if !valid(value) {
			return value
		}
		count++
		return replacement
	}), count
}

func likelyPhoneNumber(value string) bool {
	digits := 0
	for _, character := range value {
		if character >= '0' && character <= '9' {
			digits++
		}
	}
	if digits > 15 {
		return false
	}
	return digits >= 10 || digits >= 7 && (strings.HasPrefix(value, "+") || strings.ContainsAny(value, "()"))
}

func removeMailRoutingHeaders(text string) (string, int64) {
	separator, width := strings.Index(text, "\r\n\r\n"), 4
	if separator < 0 {
		separator, width = strings.Index(text, "\n\n"), 2
	}
	if separator <= 0 {
		return text, 0
	}
	header := text[:separator]
	newline := "\n"
	if strings.Contains(header, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(header, newline)
	recognized := 0
	for _, line := range lines {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if name, _, ok := strings.Cut(line, ":"); ok && name != "" {
			recognized++
		}
	}
	if recognized < 2 {
		return text, 0
	}
	kept := make([]string, 0, len(lines))
	drop, removed := false, int64(0)
	for _, line := range lines {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if !drop {
				kept = append(kept, line)
			}
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		drop = ok && mailRoutingHeaders[strings.ToLower(strings.TrimSpace(name))]
		if drop {
			removed++
		} else {
			kept = append(kept, line)
		}
	}
	if removed == 0 {
		return text, 0
	}
	return strings.Join(kept, newline) + text[separator:separator+width] + text[separator+width:], removed
}

func privacyRedactionForRow(row shard.TextRow) privacyRedaction {
	return privacyRedaction{
		EmailAddresses: row.RedactedEmailAddresses, IPAddresses: row.RedactedIPAddresses,
		PhoneNumbers: row.RedactedPhoneNumbers, MailRoutingHeaders: row.RemovedMailRoutingHeaders,
		Credentials: row.RedactedCredentials,
	}
}
