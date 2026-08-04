// Package lookaside provides verified access to content-addressed objects. It
// knows where bytes live and whether they match a hash; it never assigns those
// bytes corpus meaning.
package lookaside

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Cache struct {
	root   string
	client *http.Client
}

func NewCache(root string, client *http.Client) (*Cache, error) {
	if root == "" {
		return nil, fmt.Errorf("lookaside cache root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Cache{root: abs, client: client}, nil
}

func DefaultCache() (*Cache, error) {
	if root := os.Getenv("WALDO_CACHE"); root != "" {
		return NewCache(root, nil)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("find user cache directory: %w", err)
	}
	return NewCache(filepath.Join(base, "waldo", "objects"), nil)
}

func (cache *Cache) Root() string { return cache.root }

func (cache *Cache) Path(digest string) (string, error) {
	if err := validateDigest(digest); err != nil {
		return "", err
	}
	return filepath.Join(cache.root, digest[:2], digest[2:4], digest), nil
}

// Fetch returns a local verified object path. An existing cache entry is
// re-hashed before use. Downloads are streamed to a sibling temporary file and
// become visible only after their digest and optional expected size match.
func (cache *Cache) Fetch(ctx context.Context, objectURL, digest string, expectedBytes int64) (string, error) {
	destination, err := cache.Path(digest)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(destination); err == nil && !info.IsDir() {
		if err := VerifyFile(destination, digest, expectedBytes); err == nil {
			return destination, nil
		}
		// A cache entry is derived and addressed by its expected content. Once
		// it fails that identity it has no valid use, so remove it before the
		// repair fetch rather than allowing any consumer to observe it.
		if err := os.Remove(destination); err != nil {
			return "", fmt.Errorf("remove corrupt cached object %s: %w", digest, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}

	reader, err := cache.open(ctx, objectURL)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".waldo-object-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hasher), reader)
	if copyErr != nil {
		return "", fmt.Errorf("fetch %s: %w", objectURL, copyErr)
	}
	if expectedBytes > 0 && written != expectedBytes {
		return "", fmt.Errorf("fetch %s: size mismatch: got %d bytes, want %d", objectURL, written, expectedBytes)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != digest {
		return "", fmt.Errorf("fetch %s: sha256 mismatch: got %s, want %s", objectURL, got, digest)
	}
	if err := temporary.Sync(); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", err
	}
	committed = true
	return destination, nil
}

func (cache *Cache) open(ctx context.Context, objectURL string) (io.ReadCloser, error) {
	parsed, err := url.Parse(objectURL)
	if err != nil {
		return nil, fmt.Errorf("parse lookaside URL %q: %w", objectURL, err)
	}
	switch parsed.Scheme {
	case "http", "https":
		return cache.openHTTP(ctx, parsed.String())
	case "s3":
		return cache.openHTTP(ctx, s3HTTPS(parsed))
	case "file":
		path, err := url.PathUnescape(parsed.Path)
		if err != nil {
			return nil, err
		}
		return os.Open(filepath.FromSlash(path))
	case "":
		return os.Open(objectURL)
	default:
		return nil, fmt.Errorf("unsupported lookaside URL scheme %q", parsed.Scheme)
	}
}

func (cache *Cache) openHTTP(ctx context.Context, objectURL string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, objectURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := cache.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", objectURL, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("fetch %s: HTTP %s", objectURL, response.Status)
	}
	return response.Body, nil
}

func s3HTTPS(parsed *url.URL) string {
	host := parsed.Host
	if strings.HasPrefix(host, "s3.") || strings.HasPrefix(host, "s3-") || host == "s3.amazonaws.com" {
		return (&url.URL{Scheme: "https", Host: host, Path: parsed.Path, RawQuery: parsed.RawQuery}).String()
	}
	return (&url.URL{Scheme: "https", Host: host + ".s3.amazonaws.com", Path: parsed.Path, RawQuery: parsed.RawQuery}).String()
}

func VerifyFile(path, digest string, expectedBytes int64) error {
	if err := validateDigest(digest); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if expectedBytes > 0 && info.Size() != expectedBytes {
		return fmt.Errorf("%s: size mismatch: got %d bytes, want %d", path, info.Size(), expectedBytes)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	return compareDigest(path, hasher, digest)
}

func compareDigest(path string, hasher hash.Hash, want string) error {
	if got := hex.EncodeToString(hasher.Sum(nil)); got != want {
		return fmt.Errorf("%s: sha256 mismatch: got %s, want %s", path, got, want)
	}
	return nil
}

func validateDigest(digest string) error {
	if len(digest) != sha256.Size*2 {
		return fmt.Errorf("invalid sha256 %q: want 64 lowercase hexadecimal characters", digest)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || hex.EncodeToString(decoded) != digest {
		return fmt.Errorf("invalid sha256 %q: want 64 lowercase hexadecimal characters", digest)
	}
	return nil
}
