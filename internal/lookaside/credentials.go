package lookaside

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const credentialFileSchema = 1

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

// FileCredentialStore persists one bucket-scoped interactive S3 login in
// ~/.waldo/credentials. Path is injectable for tests; an empty Path selects
// the default location. The AWS SDK's environment and workload-identity chain
// remains available when this file is absent.
type FileCredentialStore struct {
	Path string
}

type credentialFile struct {
	Schema      int         `json:"schema"`
	Scope       string      `json:"scope"`
	Credentials Credentials `json:"credentials"`
}

func CredentialPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory for WALDO credentials: %w", err)
	}
	return filepath.Join(home, ".waldo", "credentials"), nil
}

func (store FileCredentialStore) path() (string, error) {
	if store.Path != "" {
		return filepath.Abs(store.Path)
	}
	return CredentialPath()
}

func (store FileCredentialStore) Get(publishURL string) (Credentials, bool, error) {
	scope, err := CredentialScope(publishURL)
	if err != nil {
		return Credentials{}, false, err
	}
	path, err := store.path()
	if err != nil {
		return Credentials{}, false, err
	}
	stored, found, err := readCredentialFile(path)
	if err != nil || !found {
		return Credentials{}, found, err
	}
	if stored.Scope != scope {
		return Credentials{}, false, nil
	}
	return stored.Credentials, true, nil
}

func (store FileCredentialStore) Set(publishURL string, credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	scope, err := CredentialScope(publishURL)
	if err != nil {
		return err
	}
	path, err := store.path()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create WALDO credential directory %s: %w", directory, err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect WALDO credential directory %s: %w", directory, err)
	}
	data, err := json.MarshalIndent(credentialFile{
		Schema: credentialFileSchema, Scope: scope, Credentials: credentials,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode WALDO credentials: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create temporary WALDO credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect temporary WALDO credential file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary WALDO credential file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary WALDO credential file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary WALDO credential file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install WALDO credential file %s: %w", path, err)
	}
	committed = true
	if err := syncCredentialDirectory(directory); err != nil {
		return err
	}
	return nil
}

func (store FileCredentialStore) Delete(publishURL string) error {
	scope, err := CredentialScope(publishURL)
	if err != nil {
		return err
	}
	path, err := store.path()
	if err != nil {
		return err
	}
	stored, found, err := readCredentialFile(path)
	if err != nil || !found || stored.Scope != scope {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove WALDO credential file %s: %w", path, err)
	}
	return syncCredentialDirectory(filepath.Dir(path))
}

func readCredentialFile(path string) (credentialFile, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return credentialFile{}, false, nil
	}
	if err != nil {
		return credentialFile{}, false, fmt.Errorf("inspect WALDO credential file %s: %w", path, err)
	}
	directory := filepath.Dir(path)
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return credentialFile{}, false, fmt.Errorf("inspect WALDO credential directory %s: %w", directory, err)
	}
	if !directoryInfo.IsDir() {
		return credentialFile{}, false, fmt.Errorf("WALDO credential directory %s must be a directory", directory)
	}
	if directoryInfo.Mode().Perm()&0o077 != 0 {
		return credentialFile{}, false, fmt.Errorf("WALDO credential directory %s is accessible by group or others; run chmod 700 %s", directory, directory)
	}
	if !info.Mode().IsRegular() {
		return credentialFile{}, false, fmt.Errorf("WALDO credential file %s must be a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return credentialFile{}, false, fmt.Errorf("WALDO credential file %s is readable by group or others; run chmod 600 %s", path, path)
	}
	if info.Size() > 64*1024 {
		return credentialFile{}, false, fmt.Errorf("WALDO credential file %s exceeds 64 KiB", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return credentialFile{}, false, fmt.Errorf("open WALDO credential file %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024))
	decoder.DisallowUnknownFields()
	var stored credentialFile
	if err := decoder.Decode(&stored); err != nil {
		return credentialFile{}, false, fmt.Errorf("decode WALDO credential file %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return credentialFile{}, false, fmt.Errorf("decode WALDO credential file %s: %w", path, err)
	}
	if stored.Schema != credentialFileSchema {
		return credentialFile{}, false, fmt.Errorf("WALDO credential file %s has unsupported schema %d", path, stored.Schema)
	}
	if stored.Scope == "" {
		return credentialFile{}, false, fmt.Errorf("WALDO credential file %s has no S3 scope", path)
	}
	if err := stored.Credentials.Validate(); err != nil {
		return credentialFile{}, false, fmt.Errorf("invalid WALDO credential file %s: %w", path, err)
	}
	return stored, true, nil
}

func syncCredentialDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open WALDO credential directory %s: %w", path, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync WALDO credential directory %s: %w", path, err)
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
