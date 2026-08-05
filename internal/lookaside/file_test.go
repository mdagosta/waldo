package lookaside

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openwaldo/waldo-new/internal/config"
)

func TestFilePublisherPublishesVerifiesAndReusesObject(t *testing.T) {
	root := t.TempDir()
	base := (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String()
	publisher, err := NewPublisher(context.Background(), config.Publish{URL: base})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("canonical parquet test object")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	source := filepath.Join(t.TempDir(), "source.parquet")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	var progress PublishProgress
	first, err := publisher.Publish(context.Background(), source, digest, int64(len(content)), func(event PublishProgress) { progress = event })
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(root, digest[:2], digest[2:4], digest)
	if first.AlreadyExists || progress.Written != int64(len(content)) || first.URL != mirrorObjectURL(base, digest) {
		t.Fatalf("publication = %+v, progress = %+v", first, progress)
	}
	if err := VerifyFile(wantPath, digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}
	second, err := publisher.Publish(context.Background(), source, digest, int64(len(content)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !second.AlreadyExists {
		t.Fatalf("second publication = %+v", second)
	}
	if _, err := publisher.Verify(context.Background(), digest, int64(len(content))); err != nil {
		t.Fatal(err)
	}
}

func TestFilePublisherRefusesCorruptExistingObject(t *testing.T) {
	root := t.TempDir()
	base := (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String()
	publisher, err := NewFilePublisher(base)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("right")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	destination := filepath.Join(root, digest[:2], digest[2:4], digest)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source.parquet")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), source, digest, int64(len(content)), nil); err == nil {
		t.Fatal("expected corrupt destination refusal")
	}
}

func TestFilePublisherRequiresAbsoluteURL(t *testing.T) {
	if _, err := NewFilePublisher("file://relative/path"); err == nil {
		t.Fatal("expected relative or hosted file URL rejection")
	}
}

func TestFilePublisherContainsAndRemovesObject(t *testing.T) {
	root := t.TempDir()
	publisher, err := NewFilePublisher((&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("remove this object")
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	destination := filepath.Join(root, digest[:2], digest[2:4], digest)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if present, err := publisher.Contains(context.Background(), digest); err != nil || !present {
		t.Fatalf("Contains() = %v, %v", present, err)
	}
	if err := publisher.Remove(context.Background(), digest); err != nil {
		t.Fatal(err)
	}
	if present, err := publisher.Contains(context.Background(), digest); err != nil || present {
		t.Fatalf("Contains() after removal = %v, %v", present, err)
	}
}

func TestFilePublisherListsCanonicalAndNoncanonicalFiles(t *testing.T) {
	root := t.TempDir()
	publisher, err := NewFilePublisher((&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String())
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	canonicalPath := filepath.Join(root, digest[:2], digest[2:4], digest)
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte("object"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}
	objects, err := publisher.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 || !objects[0].Canonical || objects[0].Name != digest || objects[0].Prefix != "--" || objects[0].StoredAt.IsZero() || !objects[0].InConfiguredLookaside || objects[1].Canonical || !objects[1].InConfiguredLookaside {
		t.Fatalf("objects = %+v", objects)
	}
}
