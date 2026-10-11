package report

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

var errStorageUnavailable = errors.New("report: private storage unavailable")

type COSPrivateStorage struct {
	client    *cos.Client
	secretID  string
	secretKey string
	prefix    string
	timeout   time.Duration
}

func NewCOSPrivateStorage(client *cos.Client, secretID, secretKey, prefix string, timeout time.Duration) (*COSPrivateStorage, error) {
	if client == nil || strings.TrimSpace(secretID) == "" || strings.TrimSpace(secretKey) == "" || timeout <= 0 || timeout > 30*time.Second {
		return nil, ErrInvalidInput
	}
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	return &COSPrivateStorage{client: client, secretID: secretID, secretKey: secretKey, prefix: prefix, timeout: timeout}, nil
}

func (s *COSPrivateStorage) UploadPrivate(ctx context.Context, key string, data []byte) error {
	key, ok := s.objectKey(key)
	if !ok || len(data) == 0 || len(data) > 64<<20 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return ErrInvalidInput
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	_, err := s.client.Object.Put(callCtx, key, bytes.NewReader(data), &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{ContentType: "application/pdf"},
		ACLHeaderOptions:       &cos.ACLHeaderOptions{XCosACL: "private"},
	})
	if err != nil {
		return errStorageUnavailable
	}
	return nil
}

func (s *COSPrivateStorage) SignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	key, ok := s.objectKey(key)
	if !ok || ttl <= 0 || ttl > time.Hour {
		return "", ErrInvalidInput
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	url, err := s.client.Object.GetPresignedURL(callCtx, http.MethodGet, key, s.secretID, s.secretKey, ttl, nil, true)
	if err != nil {
		return "", errStorageUnavailable
	}
	return url.String(), nil
}

func (s *COSPrivateStorage) objectKey(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "..") {
		return "", false
	}
	clean := path.Clean(value)
	if clean == "." || clean != value {
		return "", false
	}
	if s.prefix != "" {
		clean = path.Join(s.prefix, clean)
	}
	return clean, true
}
