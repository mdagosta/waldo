package lookaside

import (
	"context"
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

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/openwaldo/waldo-new/internal/config"
)

type s3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

type S3Publisher struct {
	api     s3API
	baseURL string
	bucket  string
	prefix  string
}

func NewS3Publisher(ctx context.Context, publish config.Publish) (*S3Publisher, error) {
	bucket, prefix, err := parseS3Base(publish.URL)
	if err != nil {
		return nil, err
	}
	options := []func(*awsconfig.LoadOptions) error{}
	if publish.Region != "" {
		options = append(options, awsconfig.WithRegion(publish.Region))
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

func newS3PublisherWithAPI(api s3API, baseURL string) (*S3Publisher, error) {
	bucket, prefix, err := parseS3Base(baseURL)
	if err != nil {
		return nil, err
	}
	return &S3Publisher{api: api, baseURL: strings.TrimRight(baseURL, "/"), bucket: bucket, prefix: prefix}, nil
}

func (publisher *S3Publisher) BaseURL() string { return publisher.baseURL }

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
