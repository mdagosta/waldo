// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/openwaldo/waldo/internal/config"
	"github.com/openwaldo/waldo/internal/lookaside"
	"golang.org/x/term"
)

var lookasideCredentialStore lookaside.CredentialStore = lookaside.FileCredentialStore{}
var validateS3Credentials = lookaside.ValidateS3Credentials

var promptS3Credentials = func(output io.Writer) (lookaside.Credentials, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return lookaside.Credentials{}, fmt.Errorf("lookaside login requires an interactive terminal")
	}
	fmt.Fprint(output, "S3 access key: ")
	var accessKey string
	if _, err := fmt.Fscanln(os.Stdin, &accessKey); err != nil {
		return lookaside.Credentials{}, fmt.Errorf("read S3 access key: %w", err)
	}
	fmt.Fprint(output, "S3 secret key: ")
	secretKey, err := term.ReadPassword(fd)
	fmt.Fprintln(output)
	if err != nil {
		return lookaside.Credentials{}, fmt.Errorf("read S3 secret key: %w", err)
	}
	credentials := lookaside.Credentials{AccessKey: strings.TrimSpace(accessKey), SecretKey: string(secretKey)}
	if err := credentials.Validate(); err != nil {
		return lookaside.Credentials{}, err
	}
	return credentials, nil
}

func runLookasideLogin(context Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo lookaside login [--json]"}
	}
	publish, scope, err := configuredS3Lookaside()
	if err != nil {
		return err
	}
	credentials, err := promptS3Credentials(stderr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "validating S3 write, read, and delete access at %s...\n", scope)
	if err := validateS3Credentials(context.Execution, publish, credentials); err != nil {
		return err
	}
	if err := lookasideCredentialStore.Set(publish.URL, credentials); err != nil {
		return err
	}
	credentialPath, err := lookaside.CredentialPath()
	if err != nil {
		return err
	}
	redacted := lookaside.RedactAccessKey(credentials.AccessKey)
	if context.JSON {
		return writeJSON(stdout, struct {
			Scope     string `json:"scope"`
			AccessKey string `json:"access_key"`
			Store     string `json:"store"`
			Path      string `json:"path"`
			Status    string `json:"status"`
		}{Scope: scope, AccessKey: redacted, Store: "waldo-credential-file", Path: credentialPath, Status: "verified-and-stored"})
	}
	fmt.Fprintf(stdout, "verified S3 access and stored credentials for %s in %s (%s)\n", scope, credentialPath, redacted)
	return nil
}

func runLookasideLogout(context Context, args []string, stdout, _ io.Writer) error {
	if len(args) != 0 {
		return usageError{message: "usage: waldo lookaside logout [--json]"}
	}
	publish, scope, err := configuredS3Lookaside()
	if err != nil {
		return err
	}
	if err := lookasideCredentialStore.Delete(publish.URL); err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, struct {
			Scope  string `json:"scope"`
			Status string `json:"status"`
		}{Scope: scope, Status: "logged-out"})
	}
	credentialPath, err := lookaside.CredentialPath()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "removed S3 credentials for %s from %s\n", scope, credentialPath)
	return nil
}

func configuredS3Lookaside() (config.Publish, string, error) {
	configuration, err := config.Load()
	if err != nil {
		return config.Publish{}, "", err
	}
	if configuration.Lookaside.Publish == nil {
		return config.Publish{}, "", usageError{message: "configure an S3 lookaside before login: waldo config set lookaside s3://bucket/prefix"}
	}
	publish := *configuration.Lookaside.Publish
	parsed, err := url.Parse(publish.URL)
	if err != nil || parsed.Scheme != "s3" {
		return config.Publish{}, "", usageError{message: "lookaside login requires a configured s3:// lookaside"}
	}
	scope, err := lookaside.CredentialScope(publish.URL)
	if err != nil {
		return config.Publish{}, "", err
	}
	return publish, scope, nil
}

type lookasideCredentialStatus struct {
	Scope     string `json:"scope"`
	Source    string `json:"source"`
	Present   bool   `json:"present"`
	AccessKey string `json:"access_key,omitempty"`
	Error     string `json:"error,omitempty"`
}

func credentialStatus(publish *config.Publish) *lookasideCredentialStatus {
	if publish == nil {
		return nil
	}
	parsed, err := url.Parse(publish.URL)
	if err != nil || parsed.Scheme != "s3" {
		return nil
	}
	scope, err := lookaside.CredentialScope(publish.URL)
	if err != nil {
		return &lookasideCredentialStatus{Source: "waldo-credential-file", Error: err.Error()}
	}
	credentials, found, err := lookasideCredentialStore.Get(publish.URL)
	if err != nil {
		return &lookasideCredentialStatus{Scope: scope, Source: "waldo-credential-file", Error: err.Error()}
	}
	if !found {
		return &lookasideCredentialStatus{Scope: scope, Source: "aws-default-chain"}
	}
	return &lookasideCredentialStatus{Scope: scope, Source: "waldo-credential-file", Present: true, AccessKey: lookaside.RedactAccessKey(credentials.AccessKey)}
}
