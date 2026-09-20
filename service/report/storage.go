package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
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
	digest := sha256.Sum256(data)
	checksum := hex.EncodeToString(digest[:])
	metadata := http.Header{}
	metadata.Set("x-cos-meta-report-sha256", checksum)
	_, err := s.client.Object.Put(callCtx, key, bytes.NewReader(data), &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{ContentType: "application/pdf", XCosMetaXXX: &metadata},
		ACLHeaderOptions:       &cos.ACLHeaderOptions{XCosACL: "private"},
	})
	if err != nil {
		return errStorageUnavailable
	}
	// A successful PUT alone is not the delivery gate: verify the stored object
	// before allowing the worker to publish its download reference.
	verifyCtx, verifyCancel := context.WithTimeout(ctx, s.timeout)
	defer verifyCancel()
	response, err := s.client.Object.Head(verifyCtx, key, nil)
	if err != nil || response == nil || response.Header.Get("x-cos-meta-report-sha256") != checksum || response.Header.Get("Content-Length") != strconv.Itoa(len(data)) {
		return errStorageUnavailable
	}
	// Metadata is supplied by this uploader; it does not prove stored bytes.
	// Read back the bounded private artifact before publishing the reference.
	object, err := s.client.Object.Get(verifyCtx, key, nil)
	if err != nil || object == nil || object.Body == nil {
		return errStorageUnavailable
	}
	defer object.Body.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(object.Body, int64(len(data))+1))
	if err != nil || size != int64(len(data)) || hex.EncodeToString(hash.Sum(nil)) != checksum {
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
