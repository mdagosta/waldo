package lookaside

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/openwaldo/waldo-new/internal/config"
)

type ListedObject struct {
	Name                  string    `json:"name,omitempty"`
	Bytes                 int64     `json:"bytes"`
	Path                  string    `json:"path"`
	Prefix                string    `json:"prefix"`
	Canonical             bool      `json:"canonical"`
	InConfiguredLookaside bool      `json:"in_configured_lookaside"`
	StoredAt              time.Time `json:"stored_at"`
}

type ObjectLister interface {
	BaseURL() string
	InventoryURL() string
	List(context.Context) ([]ListedObject, error)
}

func NewObjectLister(ctx context.Context, publish config.Publish) (ObjectLister, error) {
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
		return nil, fmt.Errorf("unsupported lookaside listing scheme %q", parsed.Scheme)
	}
}

func classifyObjectPath(relativePath string) (name, prefix string, canonical bool) {
	trimmed := strings.Trim(relativePath, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return "", trimmed, false
	}
	fanout := parts[len(parts)-3:]
	digest := fanout[2]
	if validateDigest(digest) != nil || fanout[0] != digest[:2] || fanout[1] != digest[2:4] {
		return "", trimmed, false
	}
	prefix = strings.Join(parts[:len(parts)-3], "/")
	if prefix == "" {
		prefix = "--"
	}
	return digest, prefix, true
}

func sortListedObjects(objects []ListedObject) {
	sort.Slice(objects, func(i, j int) bool { return objects[i].Path < objects[j].Path })
}
