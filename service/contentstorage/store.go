package contentstorage

import (
	"context"
	"errors"
)

const MaxObjectSize int64 = 50 << 20

var (
	ErrUnavailable         = errors.New("content storage unavailable")
	ErrInvalidInput        = errors.New("invalid content storage input")
	ErrNotFound            = errors.New("content object not found")
	ErrConflict            = errors.New("content object conflicts with existing data")
	ErrPublicObject        = errors.New("content object is publicly accessible")
	ErrIntegrity           = errors.New("content object integrity check failed")
	ErrRangeNotSatisfiable = errors.New("content range not satisfiable")
)

type RangeError struct {
	Size int64
}

func (e *RangeError) Error() string { return ErrRangeNotSatisfiable.Error() }
func (e *RangeError) Unwrap() error { return ErrRangeNotSatisfiable }

type Metadata struct {
	Size   int64
	MIME   string
	SHA256 string
}

type Store interface {
	Put(ctx context.Context, tenant, assetID string, data []byte, mime string) error
	Get(ctx context.Context, tenant, assetID string, max int64) ([]byte, string, error)
	Head(ctx context.Context, tenant, assetID string) (Metadata, error)
}
