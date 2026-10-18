package storage

import (
	"context"
	"io"
	"time"
)

type ObjectInfo struct {
	Size        int64
	ContentType string
	Metadata    map[string]string
}

type Store interface {
	PresignPut(ctx context.Context, key, contentType, checksum string, size int64, expires time.Duration) (string, map[string]string, error)
	Head(ctx context.Context, key string) (ObjectInfo, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
