// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package ingest

import "regexp"

// emailAddressPattern deliberately detects common Internet email-shaped
// strings. It is a content flag, not a claim that the value identifies a
// natural person or that every RFC 5322 address is recognized.
var emailAddressPattern = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`)

func containsEmailAddress(text string) bool {
	return emailAddressPattern.MatchString(text)
}
