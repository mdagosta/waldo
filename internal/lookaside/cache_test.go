package lookaside

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchVerifiesAndCachesHTTPObject(t *testing.T) {
	content := "verified object"
	digest := digestOf(content)
	transport := &fakeTransport{content: content}
	cache, err := NewCache(t.TempDir(), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}

	path, err := cache.Fetch(context.Background(), "https://objects.example/item", digest, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content || transport.requests != 1 {
		t.Fatalf("first fetch = %q, requests = %d", data, transport.requests)
	}

	if _, err := cache.Fetch(context.Background(), "https://objects.example/item", digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}
	if transport.requests != 1 {
		t.Fatalf("cache hit made %d HTTP requests, want 1", transport.requests)
	}
}

func TestFetchRejectsWrongDigestWithoutCacheEntry(t *testing.T) {
	cache, err := NewCache(t.TempDir(), &http.Client{Transport: &fakeTransport{content: "wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	digest := digestOf("right")
	if _, err := cache.Fetch(context.Background(), "https://objects.example/item", digest, 0); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("Fetch() error = %v", err)
	}
	path, _ := cache.Path(digest)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid object was left at cache path: %v", err)
	}
}

func TestFetchRepairsCorruptCacheEntry(t *testing.T) {
	content := "correct"
	digest := digestOf(content)
	transport := &fakeTransport{content: content}
	cache, err := NewCache(t.TempDir(), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := cache.Path(digest)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Fetch(context.Background(), "https://objects.example/item", digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}
	if transport.requests != 1 {
		t.Fatalf("repair made %d requests, want 1", transport.requests)
	}
}

func TestAdmitPublishesAndReusesVerifiedObject(t *testing.T) {
	root := t.TempDir()
	cache, err := NewCache(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("canonical parquet fixture")
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	source := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	destination, err := cache.Admit(context.Background(), source, digest, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(destination, digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}
	again, err := cache.Admit(context.Background(), source, digest, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if again != destination {
		t.Fatalf("second admission = %q, want %q", again, destination)
	}
}

func TestFetchFallsBackToConfiguredMirror(t *testing.T) {
	content := "from mirror"
	digest := digestOf(content)
	transport := &fallbackTransport{content: content}
	cache, err := NewCache(t.TempDir(), &http.Client{Transport: transport}, WithMirrors([]string{"https://mirror.example/lookaside/v1"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Fetch(context.Background(), "https://primary.example/object", digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}
	wantMirror := "https://mirror.example/lookaside/v1/" + digest[:2] + "/" + digest[2:4] + "/" + digest
	if len(transport.urls) != 2 || transport.urls[0] != "https://primary.example/object" || transport.urls[1] != wantMirror {
		t.Fatalf("request order = %v, want primary then %s", transport.urls, wantMirror)
	}
}

func TestS3URLTranslation(t *testing.T) {
	tests := map[string]string{
		"s3://bucket/key": "https://bucket.s3.amazonaws.com/key",
		"s3://s3.us-east-2.amazonaws.com/bucket/key":            "https://s3.us-east-2.amazonaws.com/bucket/key",
		"s3://s3.amazonaws.com/bucket/key?versionId=identified": "https://s3.amazonaws.com/bucket/key?versionId=identified",
	}
	for input, want := range tests {
		parsed, err := url.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if got := s3HTTPS(parsed); got != want {
			t.Errorf("s3HTTPS(%q) = %q, want %q", input, got, want)
		}
	}
}

type fakeTransport struct {
	content  string
	requests int
}

type fallbackTransport struct {
	content string
	urls    []string
}

func (transport *fallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.urls = append(transport.urls, request.URL.String())
	status := http.StatusNotFound
	body := "missing"
	if request.URL.Host == "mirror.example" {
		status = http.StatusOK
		body = transport.content
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func (transport *fakeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.requests++
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(transport.content)),
		Header:     make(http.Header),
	}, nil
}

func digestOf(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
