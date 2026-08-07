package cloud

import (
	"context"
	"io"
)

type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
	ETag        string
}

type Object struct {
	ObjectInfo
	Body io.ReadCloser
}

type ObjectStore interface {
	Health(context.Context) error
	Put(context.Context, string, string, io.Reader, int64) (ObjectInfo, error)
	Get(context.Context, string) (Object, error)
	Delete(context.Context, string) error
}
