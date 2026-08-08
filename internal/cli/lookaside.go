// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/lookaside"
)

func runLookasideStatus(context Context, _ []string, stdout, _ io.Writer) error {
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	stats, err := cache.Stats()
	if err != nil {
		return err
	}
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Cache       string                     `json:"cache"`
			Scratch     string                     `json:"scratch"`
			MaxBytes    int64                      `json:"cache_max_bytes"`
			Mirrors     []string                   `json:"mirrors"`
			Publish     *config.Publish            `json:"publish,omitempty"`
			Credentials *lookasideCredentialStatus `json:"credentials,omitempty"`
			Stats       lookaside.Stats            `json:"stats"`
		}{Cache: cache.Root(), Scratch: cache.Scratch(), MaxBytes: cache.MaxBytes(), Mirrors: cache.Mirrors(), Publish: configuration.Lookaside.Publish, Credentials: credentialStatus(configuration.Lookaside.Publish), Stats: stats})
	}
	fmt.Fprintf(stdout, "lookaside cache    %s\n", cache.Root())
	fmt.Fprintf(stdout, "  limit          %s\n", humanBytes(cache.MaxBytes()))
	fmt.Fprintf(stdout, "lookaside scratch  %s\n", cache.Scratch())
	fmt.Fprintf(stdout, "  objects        %s\n", humanInteger(stats.Objects))
	fmt.Fprintf(stdout, "  bytes          %s\n", humanBytes(stats.Bytes))
	if stats.Other > 0 {
		fmt.Fprintf(stdout, "  other files    %s\n", humanInteger(stats.Other))
	}
	if len(cache.Mirrors()) == 0 {
		fmt.Fprintln(stdout, "  mirrors        (none)")
	} else {
		for i, mirror := range cache.Mirrors() {
			label := ""
			if i == 0 {
				label = "mirrors"
			}
			fmt.Fprintf(stdout, "  %-13s  %s\n", label, mirror)
		}
	}
	if configuration.Lookaside.Publish == nil {
		fmt.Fprintln(stdout, "  publish        (none)")
	} else {
		publish := configuration.Lookaside.Publish
		fmt.Fprintf(stdout, "  publish        %s (%d workers)\n", publish.URL, publish.Workers)
		if status := credentialStatus(publish); status != nil {
			switch {
			case status.Error != "":
				fmt.Fprintf(stdout, "  credentials    unavailable: %s\n", status.Error)
			case status.Present:
				credentialPath, err := lookaside.CredentialPath()
				if err != nil {
					return err
				}
				fmt.Fprintf(stdout, "  credentials    %s %s (%s)\n", credentialPath, status.Scope, status.AccessKey)
			default:
				fmt.Fprintln(stdout, "  credentials    no WALDO login; AWS default chain fallback")
			}
		}
	}
	return nil
}

func runLookasideVerify(context Context, _ []string, stdout, _ io.Writer) error {
	cache, err := lookaside.DefaultCache()
	if err != nil {
		return err
	}
	result, err := cache.Scrub()
	if err != nil {
		return err
	}
	if context.JSON {
		if err := writeJSON(stdout, struct {
			Root   string                `json:"root"`
			Result lookaside.ScrubResult `json:"result"`
		}{Root: cache.Root(), Result: result}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "scrubbed %s: %s verified, %s corrupt, %s total\n",
			cache.Root(), humanInteger(result.Verified), humanInteger(int64(len(result.Corrupt))), humanBytes(result.Bytes))
		for _, issue := range result.Corrupt {
			fmt.Fprintf(stdout, "  CORRUPT %s: %s\n", issue.Path, issue.Error)
		}
	}
	if len(result.Corrupt) > 0 {
		return fmt.Errorf("lookaside cache contains %d corrupt object(s)", len(result.Corrupt))
	}
	return nil
}
