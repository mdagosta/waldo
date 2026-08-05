package lookaside

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/openwaldo/waldo-new/internal/config"
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
}

type fakeS3 struct {
	objects map[string]fakeS3Object
	puts    int
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
