// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

// Package signing owns detached signatures for exported release documents.
package signing

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/openwaldo/waldo/internal/config"
)

const (
	WALDOBOM       = "BOM.json"
	EUBOM          = "EU-BOM.json"
	WALDOBOMBundle = "BOM.sigstore.json"
	EUBOMBundle    = "EU-BOM.sigstore.json"
)

func Configured(configuration config.Signing) bool { return configuration.Method != "" }

func SignExport(ctx context.Context, configuration config.Signing, directory string, progress io.Writer) error {
	if !Configured(configuration) {
		return fmt.Errorf("signing is not configured")
	}
	if configuration.Method == "sigstore-key" && configuration.Key == "" {
		return fmt.Errorf("signing.method is sigstore-key but signing.key is unset")
	}
	cosign, err := exec.LookPath("cosign")
	if err != nil {
		return fmt.Errorf("signing is configured but cosign is not installed or not on PATH")
	}
	for _, item := range []struct{ document, bundle string }{{WALDOBOM, WALDOBOMBundle}, {EUBOM, EUBOMBundle}} {
		document := filepath.Join(directory, item.document)
		bundle := filepath.Join(directory, item.bundle)
		arguments := []string{"sign-blob", "--yes", "--bundle", bundle}
		if configuration.Method == "sigstore-key" {
			arguments = append(arguments, "--key", configuration.Key)
		}
		arguments = append(arguments, document)
		command := exec.CommandContext(ctx, cosign, arguments...)
		command.Stdin = os.Stdin
		command.Stdout = progress
		command.Stderr = progress
		if err := command.Run(); err != nil {
			return fmt.Errorf("sign %s with %s: %w", item.document, configuration.Method, err)
		}
		info, err := os.Stat(bundle)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return fmt.Errorf("sign %s: cosign did not create a non-empty %s", item.document, item.bundle)
		}
	}
	return nil
}
