package lookaside

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/openwaldo/waldo-new/internal/config"
)

// NewPublisher selects the configured writable lookaside transport.
func NewPublisher(ctx context.Context, publish config.Publish) (Publisher, error) {
	parsed, err := url.Parse(publish.URL)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "s3":
		return NewS3Publisher(ctx, publish)
	case "file":
		return NewFilePublisher(publish.URL)
	default:
		return nil, fmt.Errorf("unsupported lookaside publisher scheme %q", parsed.Scheme)
	}
}

type FilePublisher struct {
	baseURL string
	root    string
}

func NewFilePublisher(baseURL string) (*FilePublisher, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Scheme != "file" || (parsed.Host != "" && parsed.Host != "localhost") || parsed.Path == "" || !filepath.IsAbs(filepath.FromSlash(parsed.Path)) || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("local publish URL must be an absolute file:/// URL")
	}
	root := filepath.Clean(filepath.FromSlash(parsed.Path))
	normalized := (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String()
	return &FilePublisher{baseURL: normalized, root: root}, nil
}

func (publisher *FilePublisher) BaseURL() string { return publisher.baseURL }

func (publisher *FilePublisher) Publish(ctx context.Context, source, digest string, size int64, progress func(PublishProgress)) (PublishedObject, error) {
	if err := VerifyFile(source, digest, size); err != nil {
		return PublishedObject{}, fmt.Errorf("verify publication source: %w", err)
	}
	destination, err := publisher.objectPath(digest)
	if err != nil {
		return PublishedObject{}, err
	}
	if _, err := os.Stat(destination); err == nil {
		if err := VerifyFile(destination, digest, size); err != nil {
			return PublishedObject{}, fmt.Errorf("existing local lookaside object is invalid: %w", err)
		}
		return publisher.published(digest, size, true), nil
	} else if !os.IsNotExist(err) {
		return PublishedObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return PublishedObject{}, err
	}
	input, err := os.Open(source)
	if err != nil {
		return PublishedObject{}, err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".waldo-publish-*")
	if err != nil {
		return PublishedObject{}, err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	hasher := sha256.New()
	reader := io.Reader(&contextReader{ctx: ctx, reader: input})
	if progress != nil {
		reader = &progressReader{reader: reader, total: size, progress: progress}
	}
	written, err := io.Copy(io.MultiWriter(temporary, hasher), reader)
	if err != nil {
		return PublishedObject{}, err
	}
	if written != size || hex.EncodeToString(hasher.Sum(nil)) != digest {
		return PublishedObject{}, fmt.Errorf("publication source changed while it was copied")
	}
	if err := temporary.Chmod(0o644); err != nil {
		return PublishedObject{}, err
	}
	if err := temporary.Sync(); err != nil {
		return PublishedObject{}, err
	}
	if err := temporary.Close(); err != nil {
		return PublishedObject{}, err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if verifyErr := VerifyFile(destination, digest, size); verifyErr != nil {
			return PublishedObject{}, fmt.Errorf("publish local lookaside object: %w", err)
		}
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return PublishedObject{}, err
	}
	if err := VerifyFile(destination, digest, size); err != nil {
		return PublishedObject{}, fmt.Errorf("verify published local object: %w", err)
	}
	return publisher.published(digest, size, false), nil
}

func (publisher *FilePublisher) Verify(_ context.Context, digest string, size int64) (PublishedObject, error) {
	destination, err := publisher.objectPath(digest)
	if err != nil {
		return PublishedObject{}, err
	}
	if err := VerifyFile(destination, digest, size); err != nil {
		return PublishedObject{}, err
	}
	return publisher.published(digest, size, true), nil
}

func (publisher *FilePublisher) objectPath(digest string) (string, error) {
	if err := validateDigest(digest); err != nil {
		return "", err
	}
	return filepath.Join(publisher.root, digest[:2], digest[2:4], digest), nil
}

func (publisher *FilePublisher) published(digest string, size int64, exists bool) PublishedObject {
	return PublishedObject{URL: mirrorObjectURL(publisher.baseURL, digest), SHA256: digest, Bytes: size, AlreadyExists: exists}
}
