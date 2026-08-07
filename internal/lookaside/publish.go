// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package lookaside

import (
	"context"
)

type PublishProgress struct {
	Written int64
	Total   int64
}

type PublishedObject struct {
	URL           string `json:"url"`
	SHA256        string `json:"sha256"`
	Bytes         int64  `json:"bytes"`
	AlreadyExists bool   `json:"already_exists,omitempty"`
}

// Publisher writes and independently verifies immutable content-addressed
// objects. A successful return means the remote object is safe to reference.
type Publisher interface {
	BaseURL() string
	Publish(context.Context, string, string, int64, func(PublishProgress)) (PublishedObject, error)
	Verify(context.Context, string, int64) (PublishedObject, error)
}
