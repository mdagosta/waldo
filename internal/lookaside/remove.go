// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package lookaside

import (
	"context"
	"fmt"
	"net/url"

	"github.com/openwaldo/waldo/internal/config"
)

// ObjectRemover deletes only explicitly named content-addressed objects.
type ObjectRemover interface {
	BaseURL() string
	Contains(context.Context, string) (bool, error)
	Remove(context.Context, string) error
}

func NewObjectRemover(ctx context.Context, publish config.Publish) (ObjectRemover, error) {
	parsed, err := url.Parse(publish.URL)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "s3":
		return NewS3Publisher(ctx, publish)
	case "file":
		return NewFilePublisher(publish.URL)
	default:
		return nil, fmt.Errorf("unsupported lookaside removal scheme %q", parsed.Scheme)
	}
}

func ValidateObjectName(name string) error {
	return validateDigest(name)
}
