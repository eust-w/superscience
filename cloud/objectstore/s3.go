package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"unicode"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"superscience/cloud"
)

const maxObjectBytes int64 = 256 * 1024 * 1024

type S3Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	PathStyle    bool
	EnsureBucket bool
}

type S3 struct {
	client *minio.Client
	bucket string
	region string
}

var _ cloud.ObjectStore = (*S3)(nil)

func NewS3(ctx context.Context, config S3Config) (*S3, error) {
	endpoint, secure, err := validateEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	bucket := strings.TrimSpace(config.Bucket)
	if bucket == "" {
		return nil, errors.New("S3 bucket is required")
	}
	if strings.TrimSpace(config.AccessKey) == "" || strings.TrimSpace(config.SecretKey) == "" {
		return nil, errors.New("S3 access key and secret key are required")
	}
	region := strings.TrimSpace(config.Region)
	if region == "" {
		region = "us-east-1"
	}
	options := &minio.Options{
		Creds:  credentials.NewStaticV4(strings.TrimSpace(config.AccessKey), strings.TrimSpace(config.SecretKey), strings.TrimSpace(config.SessionToken)),
		Secure: secure, Region: region,
	}
	if config.PathStyle {
		options.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	client.SetAppInfo("superscience-cloud", "dev")
	result := &S3{client: client, bucket: bucket, region: region}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check S3 bucket: %w", err)
	}
	if !exists {
		if !config.EnsureBucket {
			return nil, fmt.Errorf("S3 bucket %q does not exist", bucket)
		}
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("create S3 bucket: %w", err)
		}
	}
	return result, nil
}

func (s *S3) Health(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket health: %w", err)
	}
	if !exists {
		return fmt.Errorf("S3 bucket %q is unavailable", s.bucket)
	}
	return nil
}

func (s *S3) Put(ctx context.Context, key, contentType string, reader io.Reader, size int64) (cloud.ObjectInfo, error) {
	key, err := validateKey(key)
	if err != nil {
		return cloud.ObjectInfo{}, err
	}
	if size < 0 || size > maxObjectBytes {
		return cloud.ObjectInfo{}, fmt.Errorf("S3 object size must be between 0 and %d bytes", maxObjectBytes)
	}
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if strings.ContainsAny(contentType, "\r\n") {
		return cloud.ObjectInfo{}, errors.New("S3 object content type contains a line break")
	}
	upload, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return cloud.ObjectInfo{}, fmt.Errorf("put S3 object: %w", err)
	}
	return cloud.ObjectInfo{Key: key, Size: upload.Size, ContentType: contentType, ETag: strings.Trim(upload.ETag, `"`)}, nil
}

func (s *S3) Get(ctx context.Context, key string) (cloud.Object, error) {
	key, err := validateKey(key)
	if err != nil {
		return cloud.Object{}, err
	}
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return cloud.Object{}, fmt.Errorf("stat S3 object: %w", err)
	}
	body, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return cloud.Object{}, fmt.Errorf("get S3 object: %w", err)
	}
	return cloud.Object{
		ObjectInfo: cloud.ObjectInfo{Key: key, Size: stat.Size, ContentType: stat.ContentType, ETag: strings.Trim(stat.ETag, `"`)},
		Body:       body,
	}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	key, err := validateKey(key)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func validateEndpoint(value string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false, errors.New("S3 endpoint must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false, errors.New("S3 endpoint must not contain a path")
	}
	secure := parsed.Scheme == "https"
	if !secure {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if parsed.Scheme != "http" || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return "", false, errors.New("S3 endpoint must use HTTPS except for loopback development endpoints")
		}
	}
	return parsed.Host, secure, nil
}

func validateKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return "", errors.New("S3 object key is invalid")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("S3 object key contains an invalid path segment")
		}
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", errors.New("S3 object key contains a control character")
		}
	}
	return value, nil
}
