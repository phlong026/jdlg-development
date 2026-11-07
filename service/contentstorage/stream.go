package contentstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

type Stream struct {
	Body         io.ReadCloser
	Status       int
	Size         int64
	Length       int64
	MIME         string
	ContentRange string
}

type byteSelection struct {
	start       int64
	end         int64
	length      int64
	status      int
	rangeHeader string
}

func (s *COSStore) Open(ctx context.Context, tenant, assetID, rangeHeader string) (*Stream, error) {
	key, err := s.ObjectKey(tenant, assetID)
	if err != nil {
		return nil, err
	}
	metadata, err := s.headKey(ctx, key)
	if err != nil {
		return nil, err
	}
	selection, err := parseByteRange(rangeHeader, metadata.Size)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	headers.Set("Accept-Encoding", "identity")
	options := &cos.ObjectGetOptions{XOptionHeader: &headers}
	if selection.rangeHeader != "" {
		options.Range = selection.rangeHeader
	}
	streamCtx, cancel := context.WithCancel(ctx)
	resp, getErr := s.client.Object.Get(streamCtx, key, options)
	if getErr != nil {
		cancel()
		status := responseStatus(resp)
		closeResponse(resp)
		if status == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, sanitizedCallError(ctx, "open object")
	}
	if err := validateStreamResponse(resp, metadata, selection); err != nil {
		cancel()
		closeResponse(resp)
		return nil, err
	}
	expectedHash := ""
	if selection.status == http.StatusOK {
		expectedHash = metadata.SHA256
	}
	body := newValidatingStreamBody(newIdleReadCloser(resp.Body, cancel, s.idleTimeout), selection.length, expectedHash)
	return &Stream{
		Body:         body,
		Status:       selection.status,
		Size:         metadata.Size,
		Length:       selection.length,
		MIME:         metadata.MIME,
		ContentRange: resp.Header.Get("Content-Range"),
	}, nil
}

type idleReadCloser struct {
	source io.ReadCloser
	cancel context.CancelFunc
	timer  *time.Timer
	idle   time.Duration
	once   sync.Once
}

func newIdleReadCloser(source io.ReadCloser, cancel context.CancelFunc, idle time.Duration) *idleReadCloser {
	if idle <= 0 {
		idle = defaultTimeout
	}
	body := &idleReadCloser{source: source, cancel: cancel, idle: idle}
	body.timer = time.AfterFunc(idle, cancel)
	return body
}

func (b *idleReadCloser) Read(buffer []byte) (int, error) {
	n, err := b.source.Read(buffer)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	return n, err
}

func (b *idleReadCloser) Close() error {
	var err error
	b.once.Do(func() {
		b.timer.Stop()
		b.cancel()
		err = b.source.Close()
	})
	return err
}

func parseByteRange(header string, size int64) (byteSelection, error) {
	if header == "" {
		return byteSelection{end: size - 1, length: size, status: http.StatusOK}, nil
	}
	rangeError := func() (byteSelection, error) {
		return byteSelection{}, &RangeError{Size: size}
	}
	if size == 0 || strings.TrimSpace(header) != header || !strings.HasPrefix(header, "bytes=") {
		return rangeError()
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if spec == "" || strings.Contains(spec, ",") || strings.Count(spec, "-") != 1 {
		return rangeError()
	}
	parts := strings.SplitN(spec, "-", 2)
	var start, end int64
	if parts[0] == "" {
		suffix, ok := parseDecimal(parts[1])
		if !ok || suffix == 0 {
			return rangeError()
		}
		if suffix > size {
			suffix = size
		}
		start, end = size-suffix, size-1
	} else {
		var ok bool
		start, ok = parseDecimal(parts[0])
		if !ok || start >= size {
			return rangeError()
		}
		if parts[1] == "" {
			end = size - 1
		} else {
			end, ok = parseDecimal(parts[1])
			if !ok || end < start {
				return rangeError()
			}
			if end >= size {
				end = size - 1
			}
		}
	}
	length := end - start + 1
	return byteSelection{
		start: start, end: end, length: length, status: http.StatusPartialContent,
		rangeHeader: "bytes=" + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10),
	}, nil
}

func parseDecimal(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil
}

func validateStreamResponse(resp *cos.Response, metadata Metadata, selection byteSelection) error {
	if resp == nil || resp.Response == nil || resp.StatusCode != selection.status {
		return ErrIntegrity
	}
	length, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil || length != selection.length {
		return ErrIntegrity
	}
	if resp.Header.Get("Content-Type") != metadata.MIME {
		return ErrIntegrity
	}
	contentRange := resp.Header.Get("Content-Range")
	if selection.status == http.StatusOK {
		if contentRange != "" {
			return ErrIntegrity
		}
		return nil
	}
	want := "bytes " + strconv.FormatInt(selection.start, 10) + "-" + strconv.FormatInt(selection.end, 10) + "/" + strconv.FormatInt(metadata.Size, 10)
	if contentRange != want {
		return ErrIntegrity
	}
	return nil
}

type validatingStreamBody struct {
	source       io.ReadCloser
	remaining    int64
	expectedHash string
	hasher       hash.Hash
	done         bool
}

func newValidatingStreamBody(source io.ReadCloser, length int64, expectedHash string) *validatingStreamBody {
	body := &validatingStreamBody{source: source, remaining: length, expectedHash: expectedHash}
	if expectedHash != "" {
		body.hasher = sha256.New()
	}
	return body
}

func (b *validatingStreamBody) Read(buffer []byte) (int, error) {
	if b.done {
		return 0, io.EOF
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if b.remaining == 0 {
		return 0, b.finish()
	}
	if int64(len(buffer)) > b.remaining {
		buffer = buffer[:b.remaining]
	}
	n, readErr := b.source.Read(buffer)
	if n > 0 {
		b.remaining -= int64(n)
		if b.hasher != nil {
			_, _ = b.hasher.Write(buffer[:n])
		}
	}
	if readErr != nil && readErr != io.EOF {
		b.done = true
		return n, ErrUnavailable
	}
	if b.remaining > 0 {
		if readErr == io.EOF || n == 0 {
			b.done = true
			return n, ErrIntegrity
		}
		return n, nil
	}
	finishErr := b.finish()
	if n > 0 {
		return n, finishErr
	}
	return 0, finishErr
}

func (b *validatingStreamBody) finish() error {
	if b.done {
		return io.EOF
	}
	var extra [1]byte
	n, err := b.source.Read(extra[:])
	b.done = true
	if n != 0 {
		return ErrIntegrity
	}
	if err != io.EOF {
		return ErrUnavailable
	}
	if b.hasher != nil && hex.EncodeToString(b.hasher.Sum(nil)) != b.expectedHash {
		return ErrIntegrity
	}
	return io.EOF
}

func (b *validatingStreamBody) Close() error {
	b.done = true
	return b.source.Close()
}
