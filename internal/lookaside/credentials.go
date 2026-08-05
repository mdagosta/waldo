package lookaside

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

const credentialService = "org.openwaldo.waldo.s3"

type Credentials struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

func (credentials Credentials) Validate() error {
	if strings.TrimSpace(credentials.AccessKey) == "" || credentials.SecretKey == "" {
		return fmt.Errorf("S3 access key and secret key are required")
	}
	return nil
}

type CredentialStore interface {
	Get(string) (Credentials, bool, error)
	Set(string, Credentials) error
	Delete(string) error
}

// KeyringCredentialStore persists S3 credentials in the operating system's
// native secret store. The account identity is bucket-scoped, not prefix-scoped.
type KeyringCredentialStore struct{}

func (KeyringCredentialStore) Get(publishURL string) (Credentials, bool, error) {
	account, err := CredentialScope(publishURL)
	if err != nil {
		return Credentials{}, false, err
	}
	secret, err := keyring.Get(credentialService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return Credentials{}, false, nil
	}
	if err != nil {
		return Credentials{}, false, fmt.Errorf("read S3 credentials from OS keychain: %w", err)
	}
	var credentials Credentials
	if err := json.Unmarshal([]byte(secret), &credentials); err != nil {
		return Credentials{}, false, fmt.Errorf("decode S3 credentials from OS keychain: %w", err)
	}
	if err := credentials.Validate(); err != nil {
		return Credentials{}, false, fmt.Errorf("invalid S3 credentials in OS keychain: %w", err)
	}
	return credentials, true, nil
}

func (KeyringCredentialStore) Set(publishURL string, credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	account, err := CredentialScope(publishURL)
	if err != nil {
		return err
	}
	data, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	if err := keyring.Set(credentialService, account, string(data)); err != nil {
		return fmt.Errorf("store S3 credentials in OS keychain: %w", err)
	}
	return nil
}

func (KeyringCredentialStore) Delete(publishURL string) error {
	account, err := CredentialScope(publishURL)
	if err != nil {
		return err
	}
	if err := keyring.Delete(credentialService, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("remove S3 credentials from OS keychain: %w", err)
	}
	return nil
}

func CredentialScope(publishURL string) (string, error) {
	bucket, _, err := parseS3Base(publishURL)
	if err != nil {
		return "", err
	}
	return "s3://" + bucket, nil
}

func RedactAccessKey(accessKey string) string {
	if len(accessKey) <= 4 {
		return "****"
	}
	return "…" + accessKey[len(accessKey)-4:]
}
