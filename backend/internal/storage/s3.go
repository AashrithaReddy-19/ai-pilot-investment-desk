package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3 struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
}

func NewS3(ctx context.Context, region, bucket string) (*S3, error) {
	if strings.TrimSpace(region) == "" || strings.TrimSpace(bucket) == "" {
		return nil, errors.New("AWS_REGION and S3_DOCUMENTS_BUCKET are required for S3 storage")
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(cfg)
	return &S3{bucket: bucket, client: client, presign: s3.NewPresignClient(client)}, nil
}

func (s *S3) PresignPut(ctx context.Context, key, contentType, checksum string, size int64, expires time.Duration) (string, map[string]string, error) {
	if expires <= 0 {
		expires = 10 * time.Minute
	}
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(contentType),
		ContentLength: aws.Int64(size), ServerSideEncryption: "AES256",
		Metadata: map[string]string{"sha256": checksum},
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", nil, fmt.Errorf("presign S3 upload: %w", err)
	}
	return req.URL, map[string]string{
		"Content-Type": contentType, "Content-Length": fmt.Sprintf("%d", size),
		"x-amz-meta-sha256": checksum,
	}, nil
}

func (s *S3) Head(ctx context.Context, key string) (ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head S3 object: %w", err)
	}
	info := ObjectInfo{Metadata: out.Metadata}
	if out.ContentLength != nil {
		info.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	return info, nil
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("get S3 object: %w", err)
	}
	return out.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}
