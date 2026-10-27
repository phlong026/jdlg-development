package contentstorage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

const (
	DefaultPrivatePrefix = "jd-content-private"
	defaultTimeout       = 30 * time.Second
	maxErrorBody         = 1 << 20
	sha256MetadataHeader = "x-cos-meta-sha256"
	privateCanaryMIME    = "application/octet-stream"
	privateCanaryData    = "content-storage-private-canary-v1"
	cleanupTimeout       = 10 * time.Second
)

var (
	assetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)
	bucketPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,49}-[0-9]{5,20}$`)
	regionPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)
	prefixSegment  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Config is server-only configuration. Transports are injectable for tests and
// must never add credentials to AnonymousTransport.
type Config struct {
	Enabled            bool
	Bucket             string
	Region             string
	PrivatePrefix      string
	SecretID           string
	SecretKey          string
	SessionToken       string
	Timeout            time.Duration
	Transport          http.RoundTripper
	AnonymousTransport http.RoundTripper
}

type COSStore struct {
	enabled       bool
	client        *cos.Client
	anonymous     *http.Client
	bucketURL     *url.URL
	privatePrefix string
}

func NewCOSStore(cfg *Config) (*COSStore, error) {
	if cfg == nil || !cfg.Enabled {
		return &COSStore{}, nil
	}
	if !bucketPattern.MatchString(cfg.Bucket) || !regionPattern.MatchString(cfg.Region) {
		return nil, fmt.Errorf("configure content storage: %w", ErrInvalidInput)
	}
	if cfg.SecretID == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("configure content storage: %w", ErrUnavailable)
	}
	prefix := cfg.PrivatePrefix
	if prefix == "" {
		prefix = DefaultPrivatePrefix
	}
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < time.Second || timeout > 2*time.Minute {
		return nil, fmt.Errorf("configure content storage: %w", ErrInvalidInput)
	}

	bucketURL, err := cos.NewBucketURL(cfg.Bucket, cfg.Region, true)
	if err != nil {
		return nil, fmt.Errorf("configure content storage: %w", ErrInvalidInput)
	}
	authBase := cfg.Transport
	if authBase == nil {
		authBase = defaultTransport()
	}
	authTransport := &cos.AuthorizationTransport{
		SecretID:     cfg.SecretID,
		SecretKey:    cfg.SecretKey,
		SessionToken: cfg.SessionToken,
		Transport:    &boundedResponseTransport{next: authBase},
	}
	authClient := &http.Client{
		Timeout:       timeout,
		Transport:     authTransport,
		CheckRedirect: rejectRedirect,
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, authClient)
	client.Conf.RetryOpt.Count = 1

	anonymousBase := cfg.AnonymousTransport
	if anonymousBase == nil {
		anonymousBase = defaultTransport()
	}
	anonymous := &http.Client{
		Timeout:       timeout,
		Transport:     &boundedResponseTransport{next: anonymousBase},
		CheckRedirect: rejectRedirect,
	}

	return &COSStore{
		enabled:       true,
		client:        client,
		anonymous:     anonymous,
		bucketURL:     bucketURL,
		privatePrefix: prefix,
	}, nil
}

func (s *COSStore) ObjectKey(tenant, assetID string) (string, error) {
	if !s.enabled {
		return "", ErrUnavailable
	}
	if err := validateIdentity(tenant, assetID); err != nil {
		return "", err
	}
	tenantHash := sha256.Sum256([]byte(tenant))
	return s.privatePrefix + "/" + hex.EncodeToString(tenantHash[:]) + "/" + assetID, nil
}

func (s *COSStore) Put(ctx context.Context, tenant, assetID string, data []byte, contentType string) error {
	key, err := s.ObjectKey(tenant, assetID)
	if err != nil {
		return err
	}
	if int64(len(data)) > MaxObjectSize || validateMIME(contentType) != nil {
		return ErrInvalidInput
	}
	hash := sha256.Sum256(data)
	want := Metadata{Size: int64(len(data)), MIME: contentType, SHA256: hex.EncodeToString(hash[:])}
	if err := s.privacyPreflight(ctx); err != nil {
		return err
	}

	existing, err := s.headKey(ctx, key)
	switch {
	case err == nil:
		if existing != want {
			return ErrConflict
		}
		return s.provePrivate(ctx, key)
	case !errors.Is(err, ErrNotFound):
		return err
	}

	status, putErr := s.putPrivate(ctx, key, data, contentType, want.SHA256)
	if putErr != nil {
		if status == http.StatusPreconditionFailed || status == http.StatusConflict {
			return s.resolveWriteRace(ctx, key, want)
		}
		if status >= 200 && status < 300 {
			return s.cleanupCreatedObject(ctx, key, putErr)
		}
		return putErr
	}
	actual, err := s.headKey(ctx, key)
	if err != nil {
		return s.cleanupCreatedObject(ctx, key, err)
	}
	if actual != want {
		return s.cleanupCreatedObject(ctx, key, ErrIntegrity)
	}
	if err := s.provePrivate(ctx, key); err != nil {
		return s.cleanupCreatedObject(ctx, key, err)
	}
	return nil
}

func (s *COSStore) putPrivate(ctx context.Context, key string, data []byte, contentType, hash string) (int, error) {
	metadataHeaders := make(http.Header)
	metadataHeaders.Set(sha256MetadataHeader, hash)
	optionHeaders := make(http.Header)
	optionHeaders.Set("If-None-Match", "*")
	// COS documents this header as its native no-overwrite guard. If-None-Match
	// is sent as an additional standard precondition for compatible gateways.
	optionHeaders.Set("x-cos-forbid-overwrite", "true")
	resp, putErr := s.client.Object.Put(ctx, key, bytes.NewReader(data), &cos.ObjectPutOptions{
		ACLHeaderOptions: &cos.ACLHeaderOptions{XCosACL: "private"},
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType:   contentType,
			ContentLength: int64(len(data)),
			XCosMetaXXX:   &metadataHeaders,
			XOptionHeader: &optionHeaders,
		},
	})
	status := responseStatus(resp)
	closeResponse(resp)
	if putErr != nil {
		return status, sanitizedCallError(ctx, "put object")
	}
	return status, nil
}

func (s *COSStore) privacyPreflight(ctx context.Context) (result error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return ErrUnavailable
	}
	key := s.privatePrefix + "/_privacy-canary/" + hex.EncodeToString(nonce)
	data := []byte(privateCanaryData)
	hash := sha256.Sum256(data)
	want := Metadata{
		Size:   int64(len(data)),
		MIME:   privateCanaryMIME,
		SHA256: hex.EncodeToString(hash[:]),
	}
	status, err := s.putPrivate(ctx, key, data, privateCanaryMIME, want.SHA256)
	created := status >= 200 && status < 300
	if created {
		defer func() {
			if cleanupErr := s.deleteCreatedObject(ctx, key); cleanupErr != nil {
				result = ErrUnavailable
			}
		}()
	}
	if err != nil {
		return err
	}
	actual, err := s.headKey(ctx, key)
	if err != nil {
		return err
	}
	if actual != want {
		return ErrIntegrity
	}
	return s.provePrivate(ctx, key)
}

func (s *COSStore) cleanupCreatedObject(ctx context.Context, key string, original error) error {
	if err := s.deleteCreatedObject(ctx, key); err != nil {
		return ErrUnavailable
	}
	return original
}

func (s *COSStore) deleteCreatedObject(ctx context.Context, key string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	resp, deleteErr := s.client.Object.Delete(cleanupCtx, key)
	closeResponse(resp)
	if deleteErr != nil {
		return sanitizedCallError(cleanupCtx, "delete object")
	}
	return nil
}

func (s *COSStore) Get(ctx context.Context, tenant, assetID string, max int64) ([]byte, string, error) {
	key, err := s.ObjectKey(tenant, assetID)
	if err != nil {
		return nil, "", err
	}
	if max <= 0 || max > MaxObjectSize {
		return nil, "", ErrInvalidInput
	}
	metadata, err := s.headKey(ctx, key)
	if err != nil {
		return nil, "", err
	}
	if metadata.Size > max || metadata.Size > MaxObjectSize {
		return nil, "", ErrInvalidInput
	}
	resp, getErr := s.client.Object.Get(ctx, key, nil)
	if getErr != nil {
		status := responseStatus(resp)
		closeResponse(resp)
		if status == http.StatusNotFound {
			return nil, "", ErrNotFound
		}
		return nil, "", sanitizedCallError(ctx, "get object")
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, metadata.Size+1))
	if readErr != nil || int64(len(data)) != metadata.Size {
		return nil, "", ErrIntegrity
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != metadata.SHA256 {
		return nil, "", ErrIntegrity
	}
	if responseMIME := resp.Header.Get("Content-Type"); responseMIME != "" && responseMIME != metadata.MIME {
		return nil, "", ErrIntegrity
	}
	return data, metadata.MIME, nil
}

func (s *COSStore) Head(ctx context.Context, tenant, assetID string) (Metadata, error) {
	key, err := s.ObjectKey(tenant, assetID)
	if err != nil {
		return Metadata{}, err
	}
	return s.headKey(ctx, key)
}

func (s *COSStore) headKey(ctx context.Context, key string) (Metadata, error) {
	resp, headErr := s.client.Object.Head(ctx, key, nil)
	if headErr != nil {
		status := responseStatus(resp)
		closeResponse(resp)
		if status == http.StatusNotFound {
			return Metadata{}, ErrNotFound
		}
		return Metadata{}, sanitizedCallError(ctx, "head object")
	}
	defer resp.Body.Close()
	size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil || size < 0 || size > MaxObjectSize {
		return Metadata{}, ErrIntegrity
	}
	contentType := resp.Header.Get("Content-Type")
	if validateMIME(contentType) != nil {
		return Metadata{}, ErrIntegrity
	}
	hash := resp.Header.Get(sha256MetadataHeader)
	if len(hash) != sha256.Size*2 {
		return Metadata{}, ErrIntegrity
	}
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size || hash != strings.ToLower(hash) {
		return Metadata{}, ErrIntegrity
	}
	return Metadata{Size: size, MIME: contentType, SHA256: hash}, nil
}

func (s *COSStore) resolveWriteRace(ctx context.Context, key string, want Metadata) error {
	actual, err := s.headKey(ctx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrIntegrity) {
			return ErrConflict
		}
		return err
	}
	if actual != want {
		return ErrConflict
	}
	return s.provePrivate(ctx, key)
}

func (s *COSStore) provePrivate(ctx context.Context, key string) error {
	objectURL := *s.bucketURL
	objectURL.Path = "/" + key
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, objectURL.String(), nil)
	if err != nil {
		return ErrUnavailable
	}
	resp, err := s.anonymous.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound:
		return nil
	case http.StatusOK:
		return ErrPublicObject
	default:
		return ErrUnavailable
	}
}

func validateIdentity(tenant, assetID string) error {
	if tenant == "" || len(tenant) > 256 || !utf8.ValidString(tenant) || strings.TrimSpace(tenant) != tenant {
		return ErrInvalidInput
	}
	if !assetIDPattern.MatchString(assetID) {
		return ErrInvalidInput
	}
	return nil
}

func validatePrefix(prefix string) error {
	if len(prefix) == 0 || len(prefix) > 128 || strings.Trim(prefix, "/") != prefix {
		return fmt.Errorf("configure content storage: %w", ErrInvalidInput)
	}
	for _, segment := range strings.Split(prefix, "/") {
		if !prefixSegment.MatchString(segment) || segment == "." || segment == ".." || strings.EqualFold(segment, "ai_assets") {
			return fmt.Errorf("configure content storage: %w", ErrInvalidInput)
		}
	}
	return nil
}

func validateMIME(contentType string) error {
	if contentType == "" || len(contentType) > 255 || strings.ContainsAny(contentType, "\r\n") {
		return ErrInvalidInput
	}
	_, _, err := mime.ParseMediaType(contentType)
	return err
}

func sanitizedCallError(ctx context.Context, operation string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%s: %w", operation, ErrUnavailable)
}

func responseStatus(resp *cos.Response) int {
	if resp == nil || resp.Response == nil {
		return 0
	}
	return resp.StatusCode
}

func closeResponse(resp *cos.Response) {
	if resp != nil && resp.Response != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func rejectRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func defaultTransport() http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.ResponseHeaderTimeout = defaultTimeout
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = time.Second
	return transport
}

type boundedResponseTransport struct{ next http.RoundTripper }

func (t *boundedResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	limit := int64(maxErrorBody)
	if req.Method == http.MethodGet && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		limit = MaxObjectSize + 1
	}
	resp.Body = &limitedReadCloser{Reader: io.LimitReader(resp.Body, limit), Closer: resp.Body}
	return resp, nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}
