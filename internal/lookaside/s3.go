package lookaside

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/openwaldo/waldo/internal/config"
)

type s3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3CredentialAPI interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

type s3ListAPI interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

type S3Publisher struct {
	api     s3API
	baseURL string
	bucket  string
	prefix  string
}

func NewS3Publisher(ctx context.Context, publish config.Publish) (*S3Publisher, error) {
	return newS3Publisher(ctx, publish, FileCredentialStore{})
}

func newS3Publisher(ctx context.Context, publish config.Publish, store CredentialStore) (*S3Publisher, error) {
	bucket, prefix, err := parseS3Base(publish.URL)
	if err != nil {
		return nil, err
	}
	options := []func(*awsconfig.LoadOptions) error{}
	if publish.Region != "" {
		options = append(options, awsconfig.WithRegion(publish.Region))
	}
	// A WALDO login takes precedence. When no bucket-scoped WALDO credential
	// exists, preserve the AWS SDK's environment, shared configuration, and
	// workload-identity chain.
	credentials, found, credentialErr := store.Get(publish.URL)
	if credentialErr != nil {
		return nil, credentialErr
	}
	if found {
		options = append(options, awsconfig.WithCredentialsProvider(
			awscredentials.NewStaticCredentialsProvider(credentials.AccessKey, credentials.SecretKey, ""),
		))
	}
	awsConfiguration, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(awsConfiguration, func(options *s3.Options) {
		options.UsePathStyle = false
	})
	return &S3Publisher{api: client, baseURL: strings.TrimRight(publish.URL, "/"), bucket: bucket, prefix: prefix}, nil
}

// ValidateS3Credentials proves that credentials can perform every object
// operation WALDO relies on at the configured bucket and prefix. The probe is
// unique, contains no user data, and is deleted before this function succeeds.
func ValidateS3Credentials(ctx context.Context, publish config.Publish, credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	bucket, prefix, err := parseS3Base(publish.URL)
	if err != nil {
		return err
	}
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithCredentialsProvider(awscredentials.NewStaticCredentialsProvider(credentials.AccessKey, credentials.SecretKey, "")),
	}
	if publish.Region != "" {
		options = append(options, awsconfig.WithRegion(publish.Region))
	}
	awsConfiguration, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return fmt.Errorf("load AWS configuration for credential validation: %w", err)
	}
	client := s3.NewFromConfig(awsConfiguration)
	return validateS3CredentialAPI(ctx, client, bucket, prefix)
}

func validateS3CredentialAPI(ctx context.Context, api s3CredentialAPI, bucket, prefix string) error {
	probeID := make([]byte, 16)
	if _, err := rand.Read(probeID); err != nil {
		return fmt.Errorf("create S3 credential probe name: %w", err)
	}
	content := []byte("OpenWALDO S3 credential check " + hex.EncodeToString(probeID) + "\n")
	digest := sha256.Sum256(content)
	digestHex := hex.EncodeToString(digest[:])
	key := path.Join(prefix, digestHex[:2], digestHex[2:4], digestHex)
	checksum := base64.StdEncoding.EncodeToString(digest[:])

	_, err := api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(bucket),
		Key:            aws.String(key),
		Body:           bytes.NewReader(content),
		ContentLength:  aws.Int64(int64(len(content))),
		ChecksumSHA256: aws.String(checksum),
		ContentType:    aws.String("application/octet-stream"),
	})
	if err != nil {
		return fmt.Errorf("validate S3 credentials: write probe object: %w", err)
	}

	cleanup := func(primary error) error {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, deleteErr := api.DeleteObject(cleanupContext, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		if deleteErr != nil {
			deleteErr = fmt.Errorf("delete probe object %s: %w", key, deleteErr)
		}
		return errors.Join(primary, deleteErr)
	}

	listed, err := api.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(key), MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return cleanup(fmt.Errorf("validate S3 credentials: list probe object: %w", err))
	}
	if len(listed.Contents) != 1 || aws.ToString(listed.Contents[0].Key) != key {
		return cleanup(fmt.Errorf("validate S3 credentials: probe object was absent from listing"))
	}

	head, err := api.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return cleanup(fmt.Errorf("validate S3 credentials: inspect probe object: %w", err))
	}
	if aws.ToInt64(head.ContentLength) != int64(len(content)) || aws.ToString(head.ChecksumSHA256) != checksum {
		return cleanup(fmt.Errorf("validate S3 credentials: probe metadata did not match uploaded content"))
	}

	object, err := api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return cleanup(fmt.Errorf("validate S3 credentials: read probe object: %w", err))
	}
	read, readErr := io.ReadAll(object.Body)
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil {
		return cleanup(fmt.Errorf("validate S3 credentials: read probe content: %w", errors.Join(readErr, closeErr)))
	}
	if !bytes.Equal(read, content) {
		return cleanup(fmt.Errorf("validate S3 credentials: probe content did not match uploaded content"))
	}
	if err := cleanup(nil); err != nil {
		return fmt.Errorf("validate S3 credentials: %w", err)
	}
	return nil
}

func newS3PublisherWithAPI(api s3API, baseURL string) (*S3Publisher, error) {
	bucket, prefix, err := parseS3Base(baseURL)
	if err != nil {
		return nil, err
	}
	return &S3Publisher{api: api, baseURL: strings.TrimRight(baseURL, "/"), bucket: bucket, prefix: prefix}, nil
}

func (publisher *S3Publisher) BaseURL() string      { return publisher.baseURL }
func (publisher *S3Publisher) InventoryURL() string { return "s3://" + publisher.bucket }

func (publisher *S3Publisher) Publish(ctx context.Context, source, digest string, size int64, progress func(PublishProgress)) (PublishedObject, error) {
	if err := VerifyFile(source, digest, size); err != nil {
		return PublishedObject{}, fmt.Errorf("verify upload source: %w", err)
	}
	remote, exists, err := publisher.head(ctx, digest, size)
	if err != nil {
		return PublishedObject{}, err
	}
	if exists {
		remote.AlreadyExists = true
		return remote, nil
	}
	file, err := os.Open(source)
	if err != nil {
		return PublishedObject{}, err
	}
	defer file.Close()
	checksum, err := digestBase64(digest)
	if err != nil {
		return PublishedObject{}, err
	}
	body := io.Reader(file)
	if progress != nil {
		body = &progressReader{reader: file, total: size, progress: progress}
	}
	_, err = publisher.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(publisher.bucket), Key: aws.String(publisher.key(digest)),
		Body: body, ContentLength: aws.Int64(size), ChecksumSHA256: aws.String(checksum),
		ContentType: aws.String("application/vnd.apache.parquet"),
	})
	if err != nil {
		return PublishedObject{}, fmt.Errorf("upload %s: %w", digest, err)
	}
	remote, exists, err = publisher.head(ctx, digest, size)
	if err != nil {
		return PublishedObject{}, err
	}
	if !exists {
		return PublishedObject{}, fmt.Errorf("uploaded object %s is absent during verification", digest)
	}
	return remote, nil
}

func (publisher *S3Publisher) Verify(ctx context.Context, digest string, size int64) (PublishedObject, error) {
	remote, exists, err := publisher.head(ctx, digest, size)
	if err != nil {
		return PublishedObject{}, err
	}
	if !exists {
		return PublishedObject{}, fmt.Errorf("remote object %s does not exist", digest)
	}
	return remote, nil
}

func (publisher *S3Publisher) Contains(ctx context.Context, digest string) (bool, error) {
	if err := validateDigest(digest); err != nil {
		return false, err
	}
	_, err := publisher.api.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(publisher.bucket), Key: aws.String(publisher.key(digest)), ChecksumMode: types.ChecksumModeEnabled})
	if isS3NotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect lookaside object %s: %w", digest, err)
	}
	return true, nil
}

func (publisher *S3Publisher) Remove(ctx context.Context, digest string) error {
	if err := validateDigest(digest); err != nil {
		return err
	}
	_, err := publisher.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(publisher.bucket), Key: aws.String(publisher.key(digest))})
	if err != nil {
		return fmt.Errorf("remove lookaside object %s: %w", digest, err)
	}
	return nil
}

func (publisher *S3Publisher) List(ctx context.Context) ([]ListedObject, error) {
	api, ok := publisher.api.(s3ListAPI)
	if !ok {
		return nil, fmt.Errorf("S3 client does not support object listing")
	}
	configuredPrefix := strings.Trim(publisher.prefix, "/")
	configuredPrefixWithSlash := configuredPrefix
	if configuredPrefixWithSlash != "" {
		configuredPrefixWithSlash += "/"
	}
	objects := []ListedObject{}
	var continuation *string
	for {
		output, err := api.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(publisher.bucket), ContinuationToken: continuation,
		})
		if err != nil {
			return nil, fmt.Errorf("list S3 lookaside %s: %w", publisher.baseURL, err)
		}
		for _, object := range output.Contents {
			key := aws.ToString(object.Key)
			name, objectPrefix, canonical := classifyObjectPath(key)
			inside := configuredPrefix == "" || key == configuredPrefix || strings.HasPrefix(key, configuredPrefixWithSlash)
			objects = append(objects, ListedObject{
				Name: name, Bytes: aws.ToInt64(object.Size), Path: "s3://" + publisher.bucket + "/" + key,
				Prefix: objectPrefix, Canonical: canonical, InConfiguredLookaside: inside,
				StoredAt: aws.ToTime(object.LastModified).UTC(),
			})
		}
		if !aws.ToBool(output.IsTruncated) {
			break
		}
		if output.NextContinuationToken == nil || aws.ToString(output.NextContinuationToken) == "" {
			return nil, fmt.Errorf("list S3 lookaside %s: truncated response omitted continuation token", publisher.baseURL)
		}
		continuation = output.NextContinuationToken
	}
	sortListedObjects(objects)
	return objects, nil
}

func (publisher *S3Publisher) head(ctx context.Context, digest string, size int64) (PublishedObject, bool, error) {
	if err := validateDigest(digest); err != nil {
		return PublishedObject{}, false, err
	}
	checksum, err := digestBase64(digest)
	if err != nil {
		return PublishedObject{}, false, err
	}
	output, err := publisher.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(publisher.bucket), Key: aws.String(publisher.key(digest)),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if isS3NotFound(err) {
		return PublishedObject{}, false, nil
	}
	if err != nil {
		return PublishedObject{}, false, fmt.Errorf("verify remote object %s: %w", digest, err)
	}
	if aws.ToInt64(output.ContentLength) != size {
		return PublishedObject{}, false, fmt.Errorf("remote object %s has size %d, want %d", digest, aws.ToInt64(output.ContentLength), size)
	}
	if aws.ToString(output.ChecksumSHA256) != checksum {
		return PublishedObject{}, false, fmt.Errorf("remote object %s has SHA-256 checksum %q, want %q", digest, aws.ToString(output.ChecksumSHA256), checksum)
	}
	return PublishedObject{URL: mirrorObjectURL(publisher.baseURL, digest), SHA256: digest, Bytes: size}, true, nil
}

func (publisher *S3Publisher) key(digest string) string {
	return path.Join(publisher.prefix, digest[:2], digest[2:4], digest)
}

func parseS3Base(value string) (bucket, prefix string, err error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" {
		return "", "", fmt.Errorf("publish URL must be an s3:// URL")
	}
	prefix = strings.Trim(parsed.Path, "/")
	if parsed.Host == "s3.amazonaws.com" || strings.HasPrefix(parsed.Host, "s3.") || strings.HasPrefix(parsed.Host, "s3-") {
		parts := strings.SplitN(prefix, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			return "", "", fmt.Errorf("endpoint-style S3 URL requires a bucket path")
		}
		bucket = parts[0]
		prefix = ""
		if len(parts) == 2 {
			prefix = parts[1]
		}
		return bucket, prefix, nil
	}
	return parsed.Host, prefix, nil
}

func digestBase64(digest string) (string, error) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("invalid SHA-256 digest %q", digest)
	}
	return base64.StdEncoding.EncodeToString(decoded), nil
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var response *smithyhttp.ResponseError
	return errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotFound
}

type progressReader struct {
	reader   io.Reader
	total    int64
	written  int64
	progress func(PublishProgress)
}

func (reader *progressReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.written += int64(count)
		reader.progress(PublishProgress{Written: reader.written, Total: reader.total})
	}
	return count, err
}
