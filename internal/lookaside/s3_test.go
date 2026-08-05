package lookaside

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/openwaldo/waldo/internal/config"
)

func TestS3PublisherUploadsVerifiesAndReusesObject(t *testing.T) {
	api := &fakeS3{objects: map[string]fakeS3Object{}}
	publisher, err := newS3PublisherWithAPI(api, "s3://bucket/lookaside/v1")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("parquet bytes")
	digestBytes := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytes[:])
	source := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	var progress PublishProgress
	first, err := publisher.Publish(context.Background(), source, digest, int64(len(content)), func(event PublishProgress) { progress = event })
	if err != nil {
		t.Fatal(err)
	}
	if api.puts != 1 || progress.Written != int64(len(content)) || first.URL != "s3://bucket/lookaside/v1/"+digest[:2]+"/"+digest[2:4]+"/"+digest {
		t.Fatalf("first publication = %+v, progress = %+v, puts = %d", first, progress, api.puts)
	}
	second, err := publisher.Publish(context.Background(), source, digest, int64(len(content)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if api.puts != 1 || !second.AlreadyExists {
		t.Fatalf("second publication = %+v, puts = %d", second, api.puts)
	}
}

func TestS3PublisherRefusesMismatchedRemoteChecksum(t *testing.T) {
	digest := fmt.Sprintf("%064x", 1)
	api := &fakeS3{objects: map[string]fakeS3Object{"prefix/" + digest[:2] + "/" + digest[2:4] + "/" + digest: {data: []byte("x"), checksum: "wrong"}}}
	publisher, err := newS3PublisherWithAPI(api, "s3://bucket/prefix")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Verify(context.Background(), digest, 1); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestParseEndpointStyleS3Base(t *testing.T) {
	bucket, prefix, err := parseS3Base("s3://s3.us-east-2.amazonaws.com/openwaldo/lookaside/v1")
	if err != nil || bucket != "openwaldo" || prefix != "lookaside/v1" {
		t.Fatalf("parse = %q, %q, %v", bucket, prefix, err)
	}
}

func TestS3PublisherPrefersStoredCredentials(t *testing.T) {
	store := &memoryCredentialStore{credentials: Credentials{AccessKey: "stored-access", SecretKey: "stored-secret"}, found: true}
	publisher, err := newS3Publisher(context.Background(), config.Publish{URL: "s3://bucket/prefix", Region: "us-east-2"}, store)
	if err != nil {
		t.Fatal(err)
	}
	client, ok := publisher.api.(*s3.Client)
	if !ok {
		t.Fatalf("publisher API = %T", publisher.api)
	}
	resolved, err := client.Options().Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AccessKeyID != "stored-access" || resolved.SecretAccessKey != "stored-secret" || resolved.SessionToken != "" {
		t.Fatalf("resolved credentials = %+v", resolved)
	}
}

type memoryCredentialStore struct {
	credentials Credentials
	found       bool
	err         error
}

func (store *memoryCredentialStore) Get(string) (Credentials, bool, error) {
	return store.credentials, store.found, store.err
}

func (store *memoryCredentialStore) Set(_ string, credentials Credentials) error {
	store.credentials, store.found = credentials, true
	return store.err
}

func (store *memoryCredentialStore) Delete(string) error {
	store.credentials, store.found = Credentials{}, false
	return store.err
}

type fakeS3Object struct {
	data     []byte
	checksum string
	modified time.Time
}

type fakeS3 struct {
	objects      map[string]fakeS3Object
	puts         int
	gets         int
	deletes      int
	getErr       error
	listPageSize int
}

func (api *fakeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != aws.ToInt64(input.ContentLength) {
		return nil, fmt.Errorf("content length mismatch")
	}
	api.puts++
	api.objects[aws.ToString(input.Key)] = fakeS3Object{data: bytes.Clone(data), checksum: aws.ToString(input.ChecksumSHA256)}
	return &s3.PutObjectOutput{ChecksumSHA256: input.ChecksumSHA256}, nil
}

func (api *fakeS3) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	object, ok := api.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, &types.NotFound{}
	}
	if input.ChecksumMode != types.ChecksumModeEnabled {
		return nil, fmt.Errorf("checksum mode not enabled")
	}
	checksum := object.checksum
	if checksum == "" {
		digest := sha256.Sum256(object.data)
		checksum = base64.StdEncoding.EncodeToString(digest[:])
	}
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(int64(len(object.data))), ChecksumSHA256: aws.String(checksum)}, nil
}

func (api *fakeS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if api.getErr != nil {
		return nil, api.getErr
	}
	object, ok := api.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	api.gets++
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(object.data))}, nil
}

func (api *fakeS3) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(api.objects, aws.ToString(input.Key))
	api.deletes++
	return &s3.DeleteObjectOutput{}, nil
}

func (api *fakeS3) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	keys := make([]string, 0, len(api.objects))
	for key := range api.objects {
		if strings.HasPrefix(key, aws.ToString(input.Prefix)) && (input.ContinuationToken == nil || key > aws.ToString(input.ContinuationToken)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	limit := len(keys)
	if api.listPageSize > 0 && limit > api.listPageSize {
		limit = api.listPageSize
	}
	if input.MaxKeys != nil && int(aws.ToInt32(input.MaxKeys)) < limit {
		limit = int(aws.ToInt32(input.MaxKeys))
	}
	output := &s3.ListObjectsV2Output{}
	for _, key := range keys[:limit] {
		object := api.objects[key]
		modified := object.modified
		if modified.IsZero() {
			modified = time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC)
		}
		output.Contents = append(output.Contents, types.Object{Key: aws.String(key), Size: aws.Int64(int64(len(object.data))), LastModified: aws.Time(modified)})
	}
	if limit < len(keys) {
		output.IsTruncated = aws.Bool(true)
		output.NextContinuationToken = aws.String(keys[limit-1])
	}
	return output, nil
}

func TestValidateS3CredentialsRoundTripsAndDeletesProbe(t *testing.T) {
	api := &fakeS3{objects: map[string]fakeS3Object{}}
	if err := validateS3CredentialAPI(context.Background(), api, "bucket", "lookaside/v1"); err != nil {
		t.Fatal(err)
	}
	if api.puts != 1 || api.gets != 1 || api.deletes != 1 || len(api.objects) != 0 {
		t.Fatalf("puts=%d gets=%d deletes=%d objects=%d", api.puts, api.gets, api.deletes, len(api.objects))
	}
}

func TestValidateS3CredentialsCleansUpAfterReadFailure(t *testing.T) {
	api := &fakeS3{objects: map[string]fakeS3Object{}, getErr: errors.New("read denied")}
	err := validateS3CredentialAPI(context.Background(), api, "bucket", "prefix")
	if err == nil || !strings.Contains(err.Error(), "read probe object") {
		t.Fatalf("error = %v", err)
	}
	if api.deletes != 1 || len(api.objects) != 0 {
		t.Fatalf("deletes=%d objects=%d", api.deletes, len(api.objects))
	}
}

func TestS3PublisherContainsAndRemovesExactObject(t *testing.T) {
	digest := fmt.Sprintf("%064x", 42)
	key := "prefix/" + digest[:2] + "/" + digest[2:4] + "/" + digest
	api := &fakeS3{objects: map[string]fakeS3Object{key: {data: []byte("object")}}}
	publisher, err := newS3PublisherWithAPI(api, "s3://bucket/prefix")
	if err != nil {
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

func TestS3PublisherListsCanonicalObjectsAcrossPages(t *testing.T) {
	first := fmt.Sprintf("%064x", 1)
	second := fmt.Sprintf("%064x", 2)
	api := &fakeS3{listPageSize: 1, objects: map[string]fakeS3Object{
		"prefix/" + first[:2] + "/" + first[2:4] + "/" + first:    {data: []byte("one")},
		"prefix/" + second[:2] + "/" + second[2:4] + "/" + second: {data: []byte("two")},
		"prefix/notes.txt": {data: []byte("note")},
		"elsewhere/object": {data: []byte("excluded")},
	}}
	publisher, err := newS3PublisherWithAPI(api, "s3://bucket/prefix")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := publisher.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	canonical, inside := 0, 0
	for _, object := range objects {
		if object.Canonical {
			canonical++
			if object.Prefix != "prefix" || object.StoredAt.IsZero() {
				t.Fatalf("canonical object metadata = %+v", object)
			}
		}
		if object.InConfiguredLookaside {
			inside++
		}
	}
	if len(objects) != 4 || canonical != 2 || inside != 3 || objects[0].Path != "s3://bucket/elsewhere/object" {
		t.Fatalf("objects = %+v", objects)
	}
}
