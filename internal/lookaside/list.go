package lookaside

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/openwaldo/waldo-new/internal/config"
)

type ListedObject struct {
	Name                  string `json:"name,omitempty"`
	Bytes                 int64  `json:"bytes"`
	Path                  string `json:"path"`
	Canonical             bool   `json:"canonical"`
	InConfiguredLookaside bool   `json:"in_configured_lookaside"`
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

func canonicalObjectName(relativePath string) (string, bool) {
	parts := strings.Split(strings.Trim(relativePath, "/"), "/")
	if len(parts) < 3 {
		return "", false
	}
	parts = parts[len(parts)-3:]
	digest := parts[2]
	if validateDigest(digest) != nil || parts[0] != digest[:2] || parts[1] != digest[2:4] {
		return "", false
	}
	return digest, true
}

func sortListedObjects(objects []ListedObject) {
	sort.Slice(objects, func(i, j int) bool { return objects[i].Path < objects[j].Path })
}
