// Package lookaside provides verified access to content-addressed objects. It
// knows where bytes live and whether they match a hash; it never assigns those
// bytes corpus meaning.
package lookaside

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/openwaldo/waldo-new/internal/config"
)

type Cache struct {
	root    string
	client  *http.Client
	mirrors []string
	mu      sync.Mutex
	used    map[string]bool
}

type Option func(*Cache)

func WithMirrors(mirrors []string) Option {
	return func(cache *Cache) {
		cache.mirrors = append([]string(nil), mirrors...)
	}
}

func NewCache(root string, client *http.Client, options ...Option) (*Cache, error) {
	if root == "" {
		return nil, fmt.Errorf("lookaside scratch root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	cache := &Cache{root: abs, client: client, used: map[string]bool{}}
	for _, option := range options {
		option(cache)
	}
	return cache, nil
}

func DefaultCache() (*Cache, error) {
	configuration, err := config.Load()
	if err != nil {
		return nil, err
	}
	root, err := config.EffectiveScratchRoot(configuration)
	if err != nil {
		return nil, err
	}
	return NewCache(root, nil, WithMirrors(configuration.Lookaside.Mirrors))
}

func (cache *Cache) Root() string { return cache.root }

func (cache *Cache) Mirrors() []string { return append([]string(nil), cache.mirrors...) }

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
			cache.markUsed(destination)
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

	candidates := []string{objectURL}
	for _, mirror := range cache.mirrors {
		candidates = append(candidates, mirrorObjectURL(mirror, digest))
	}
	seen := map[string]bool{}
	var failures []error
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if err := cache.fetchCandidate(ctx, candidate, destination, digest, expectedBytes); err == nil {
			cache.markUsed(destination)
			return destination, nil
		} else {
			failures = append(failures, err)
		}
	}
	return "", fmt.Errorf("object %s was unavailable from its manifest URL and %d configured mirror(s): %w", digest, len(cache.mirrors), errors.Join(failures...))
}

// PurgeUsed removes only objects successfully returned by Fetch on this Cache
// instance. Callers invoke it after the consuming operation commits; failures
// deliberately leave objects available for diagnosis and retry.
func (cache *Cache) PurgeUsed() (Stats, error) {
	cache.mu.Lock()
	paths := make([]string, 0, len(cache.used))
	for path := range cache.used {
		paths = append(paths, path)
	}
	cache.mu.Unlock()
	var purged Stats
	for _, objectPath := range paths {
		digest := filepath.Base(objectPath)
		expected, err := cache.Path(digest)
		if err != nil || filepath.Clean(objectPath) != expected {
			return purged, fmt.Errorf("refuse to purge invalid cache object path %q", objectPath)
		}
		info, err := os.Stat(objectPath)
		if err == nil {
			if err := os.Remove(objectPath); err != nil {
				return purged, err
			}
			purged.Objects++
			purged.Bytes += info.Size()
		} else if !os.IsNotExist(err) {
			return purged, err
		}
		cache.mu.Lock()
		delete(cache.used, objectPath)
		cache.mu.Unlock()
		second := filepath.Dir(objectPath)
		first := filepath.Dir(second)
		_ = os.Remove(second)
		_ = os.Remove(first)
	}
	if purged.Objects > 0 {
		if err := syncDirectory(cache.root); err != nil && !os.IsNotExist(err) {
			return purged, err
		}
	}
	return purged, nil
}

func (cache *Cache) markUsed(path string) {
	cache.mu.Lock()
	cache.used[path] = true
	cache.mu.Unlock()
}

func (cache *Cache) fetchCandidate(ctx context.Context, objectURL, destination, digest string, expectedBytes int64) error {
	reader, err := cache.open(ctx, objectURL)
	if err != nil {
		return err
	}
	defer reader.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".waldo-object-*")
	if err != nil {
		return err
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
		return fmt.Errorf("fetch %s: %w", objectURL, copyErr)
	}
	if expectedBytes > 0 && written != expectedBytes {
		return fmt.Errorf("fetch %s: size mismatch: got %d bytes, want %d", objectURL, written, expectedBytes)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != digest {
		return fmt.Errorf("fetch %s: sha256 mismatch: got %s, want %s", objectURL, got, digest)
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	committed = true
	return nil
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

func mirrorObjectURL(base, digest string) string {
	objectPath := path.Join(digest[:2], digest[2:4], digest)
	parsed, err := url.Parse(base)
	if err == nil && parsed.Scheme != "" {
		parsed.Path = path.Join(parsed.Path, objectPath)
		return parsed.String()
	}
	return filepath.Join(base, filepath.FromSlash(objectPath))
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

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
