package lookaside

import (
	"testing"

	keyring "github.com/zalando/go-keyring"
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

func TestKeyringCredentialStoreRoundTrip(t *testing.T) {
	keyring.MockInit()
	store := KeyringCredentialStore{}
	want := Credentials{AccessKey: "AKIATEST", SecretKey: "secret"}
	if err := store.Set("s3://bucket/first", want); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.Get("s3://bucket/second")
	if err != nil || !found || got != want {
		t.Fatalf("Get() = %+v, %v, %v", got, found, err)
	}
	if err := store.Delete("s3://bucket/third"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get("s3://bucket"); err != nil || found {
		t.Fatalf("Get() after delete found=%v err=%v", found, err)
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
