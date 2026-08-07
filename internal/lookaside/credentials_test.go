// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package lookaside

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialAccountIsBucketScoped(t *testing.T) {
	first, err := CredentialScope("s3://openwaldo/one/prefix")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CredentialScope("s3://openwaldo/another/prefix")
	if err != nil {
		t.Fatal(err)
	}
	if first != "s3://openwaldo" || second != first {
		t.Fatalf("accounts = %q, %q", first, second)
	}
}

func TestFileCredentialStoreRoundTripIsBucketScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".waldo", "credentials")
	store := FileCredentialStore{Path: path}
	want := Credentials{AccessKey: "AKIATEST", SecretKey: "secret"}
	if err := store.Set("s3://bucket/first", want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o, want 600", info.Mode().Perm())
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if directory.Mode().Perm() != 0o700 {
		t.Fatalf("credential directory mode = %o, want 700", directory.Mode().Perm())
	}
	got, found, err := store.Get("s3://bucket/second")
	if err != nil || !found || got != want {
		t.Fatalf("Get() = %+v, %v, %v", got, found, err)
	}
	if _, found, err := store.Get("s3://other-bucket"); err != nil || found {
		t.Fatalf("Get() for other bucket found=%v err=%v", found, err)
	}
	if err := store.Delete("s3://bucket/third"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get("s3://bucket"); err != nil || found {
		t.Fatalf("Get() after delete found=%v err=%v", found, err)
	}
}

func TestFileCredentialStoreRejectsUnsafePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	store := FileCredentialStore{Path: path}
	if err := store.Set("s3://bucket", Credentials{AccessKey: "AKIATEST", SecretKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Get("s3://bucket")
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("Get() error = %v, want chmod guidance", err)
	}
}

func TestFileCredentialStoreRejectsUnsafeDirectoryPermissions(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "credentials")
	store := FileCredentialStore{Path: path}
	if err := store.Set("s3://bucket", Credentials{AccessKey: "AKIATEST", SecretKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Get("s3://bucket")
	if err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Fatalf("Get() error = %v, want chmod guidance", err)
	}
}

func TestFileCredentialStoreRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("not credentials"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "credentials")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, _, err := (FileCredentialStore{Path: path}).Get("s3://bucket")
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("Get() error = %v, want regular-file rejection", err)
	}
}

func TestCredentialPathUsesWALDODotDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := CredentialPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".waldo", "credentials") {
		t.Fatalf("CredentialPath() = %q", path)
	}
}

func TestRedactAccessKeyShowsOnlySuffix(t *testing.T) {
	if got := RedactAccessKey("AKIAEXAMPLE1234"); got != "…1234" {
		t.Fatalf("redacted key = %q", got)
	}
	if got := RedactAccessKey("abc"); got != "****" {
		t.Fatalf("short redacted key = %q", got)
	}
}
