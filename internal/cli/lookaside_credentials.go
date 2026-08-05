package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/openwaldo/waldo-new/internal/config"
	"github.com/openwaldo/waldo-new/internal/lookaside"
	"golang.org/x/term"
)

var lookasideCredentialStore lookaside.CredentialStore = lookaside.KeyringCredentialStore{}

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
	if err := lookasideCredentialStore.Set(publish.URL, credentials); err != nil {
		return err
	}
	redacted := lookaside.RedactAccessKey(credentials.AccessKey)
	if context.JSON {
		return writeJSON(stdout, struct {
			Scope     string `json:"scope"`
			AccessKey string `json:"access_key"`
			Store     string `json:"store"`
			Status    string `json:"status"`
		}{Scope: scope, AccessKey: redacted, Store: "os-keychain", Status: "logged-in"})
	}
	fmt.Fprintf(stdout, "stored S3 credentials for %s in the OS keychain (%s)\n", scope, redacted)
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
	fmt.Fprintf(stdout, "removed S3 credentials for %s from the OS keychain\n", scope)
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
		return &lookasideCredentialStatus{Source: "os-keychain", Error: err.Error()}
	}
	credentials, found, err := lookasideCredentialStore.Get(publish.URL)
	if err != nil {
		return &lookasideCredentialStatus{Scope: scope, Source: "os-keychain", Error: err.Error()}
	}
	if !found {
		return &lookasideCredentialStatus{Scope: scope, Source: "aws-default-chain"}
	}
	return &lookasideCredentialStatus{Scope: scope, Source: "os-keychain", Present: true, AccessKey: lookaside.RedactAccessKey(credentials.AccessKey)}
}
