package contentstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseByteRange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
		size   int64
		start  int64
		end    int64
		length int64
		status int
		normal string
	}{
		{"full", "", 10, 0, 9, 10, http.StatusOK, ""},
		{"closed", "bytes=2-5", 10, 2, 5, 4, http.StatusPartialContent, "bytes=2-5"},
		{"open", "bytes=7-", 10, 7, 9, 3, http.StatusPartialContent, "bytes=7-9"},
		{"suffix", "bytes=-3", 10, 7, 9, 3, http.StatusPartialContent, "bytes=7-9"},
		{"large suffix", "bytes=-20", 10, 0, 9, 10, http.StatusPartialContent, "bytes=0-9"},
		{"clamped end", "bytes=8-99", 10, 8, 9, 2, http.StatusPartialContent, "bytes=8-9"},
		{"empty full", "", 0, 0, -1, 0, http.StatusOK, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseByteRange(tc.header, tc.size)
			if err != nil {
				t.Fatal(err)
			}
			if got.start != tc.start || got.end != tc.end || got.length != tc.length || got.status != tc.status || got.rangeHeader != tc.normal {
				t.Fatalf("selection = %#v", got)
			}
		})
	}
}

func TestParseByteRangeRejectsInvalidAndReportsSize(t *testing.T) {
	for _, header := range []string{
		"items=0-1", "bytes=", "bytes=1-2,4-5", "bytes=10-", "bytes=4-2",
		"bytes=-0", "bytes=a-2", " bytes=0-1", "bytes=0 -1",
	} {
		t.Run(header, func(t *testing.T) {
			_, err := parseByteRange(header, 10)
			if !errors.Is(err, ErrRangeNotSatisfiable) {
				t.Fatalf("error = %v", err)
			}
			var rangeErr *RangeError
			if !errors.As(err, &rangeErr) || rangeErr.Size != 10 {
				t.Fatalf("range error = %#v", rangeErr)
			}
		})
	}
}

func TestOpenStreamsNormalizedRanges(t *testing.T) {
	full := []byte("0123456789")
	hash := sha256.Sum256(full)
	for _, tc := range []struct {
		name         string
		requested    string
		normalized   string
		body         string
		contentRange string
	}{
		{"closed", "bytes=2-5", "bytes=2-5", "2345", "bytes 2-5/10"},
		{"open", "bytes=7-", "bytes=7-9", "789", "bytes 7-9/10"},
		{"suffix", "bytes=-3", "bytes=7-9", "789", "bytes 7-9/10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("Authorization") == "" {
					t.Fatal("COS request was not signed")
				}
				if calls == 1 {
					return metadataResponse(req, full, "video/mp4", hex.EncodeToString(hash[:])), nil
				}
				if req.Header.Get("Range") != tc.normalized || req.Header.Get("Accept-Encoding") != "identity" {
					t.Fatalf("GET headers = %#v", req.Header)
				}
				return streamResponse(req, http.StatusPartialContent, []byte(tc.body), "video/mp4", tc.contentRange, int64(len(tc.body))), nil
			})
			store := newTestStore(t, auth, nil)
			stream, err := store.Open(context.Background(), "tenant", testAssetID, tc.requested)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Body.Close()
			got, err := io.ReadAll(stream.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.body || stream.Status != http.StatusPartialContent || stream.Size != 10 || stream.Length != int64(len(tc.body)) || stream.ContentRange != tc.contentRange {
				t.Fatalf("stream/body = %#v %q", stream, got)
			}
		})
	}
}

func TestOpenFullStreamValidatesHashAtEOF(t *testing.T) {
	for _, tc := range []struct {
		name string
		head string
		body string
		want error
	}{
		{"valid", "complete", "complete", nil},
		{"hash mismatch", "complete", "tampered", ErrIntegrity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headData := []byte(tc.head)
			hash := sha256.Sum256(headData)
			calls := 0
			auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return metadataResponse(req, headData, "video/mp4", hex.EncodeToString(hash[:])), nil
				}
				if req.Header.Get("Range") != "" {
					t.Fatalf("unexpected Range = %q", req.Header.Get("Range"))
				}
				return streamResponse(req, http.StatusOK, []byte(tc.body), "video/mp4", "", int64(len(tc.body))), nil
			})
			store := newTestStore(t, auth, nil)
			stream, err := store.Open(context.Background(), "tenant", testAssetID, "")
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.ReadAll(stream.Body)
			_ = stream.Body.Close()
			if !errors.Is(readErr, tc.want) {
				t.Fatalf("read error = %v, want %v", readErr, tc.want)
			}
		})
	}
}

func TestOpenStreamDetectsTruncatedAndExtraBodies(t *testing.T) {
	full := []byte("0123456789")
	hash := sha256.Sum256(full)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"truncated", "234"},
		{"extra", "23456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return metadataResponse(req, full, "video/mp4", hex.EncodeToString(hash[:])), nil
				}
				return streamResponse(req, http.StatusPartialContent, []byte(tc.body), "video/mp4", "bytes 2-5/10", 4), nil
			})
			store := newTestStore(t, auth, nil)
			stream, err := store.Open(context.Background(), "tenant", testAssetID, "bytes=2-5")
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.ReadAll(stream.Body)
			_ = stream.Body.Close()
			if !errors.Is(readErr, ErrIntegrity) {
				t.Fatalf("read error = %v, want ErrIntegrity", readErr)
			}
		})
	}
}

func TestOpenRejectsInconsistentUpstreamResponseAndClosesBody(t *testing.T) {
	full := []byte("0123456789")
	hash := sha256.Sum256(full)
	for _, tc := range []struct {
		name         string
		status       int
		length       int64
		mime         string
		contentRange string
	}{
		{"status", http.StatusOK, 4, "video/mp4", "bytes 2-5/10"},
		{"length", http.StatusPartialContent, 3, "video/mp4", "bytes 2-5/10"},
		{"mime", http.StatusPartialContent, 4, "text/plain", "bytes 2-5/10"},
		{"content range", http.StatusPartialContent, 4, "video/mp4", "bytes 3-6/10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			body := &trackingReadCloser{Reader: strings.NewReader("2345")}
			auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return metadataResponse(req, full, "video/mp4", hex.EncodeToString(hash[:])), nil
				}
				resp := streamResponse(req, tc.status, nil, tc.mime, tc.contentRange, tc.length)
				resp.Body = body
				return resp, nil
			})
			store := newTestStore(t, auth, nil)
			if _, err := store.Open(context.Background(), "tenant", testAssetID, "bytes=2-5"); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("Open error = %v, want ErrIntegrity", err)
			}
			if !body.closed {
				t.Fatal("invalid upstream response body was not closed")
			}
		})
	}
}

func TestOpenRejectsRangeBeforeGET(t *testing.T) {
	data := []byte("small")
	hash := sha256.Sum256(data)
	calls := 0
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return metadataResponse(req, data, "video/mp4", hex.EncodeToString(hash[:])), nil
	})
	store := newTestStore(t, auth, nil)
	_, err := store.Open(context.Background(), "tenant", testAssetID, "bytes=99-")
	if !errors.Is(err, ErrRangeNotSatisfiable) || calls != 1 {
		t.Fatalf("Open error/calls = %v/%d", err, calls)
	}
}

func streamResponse(req *http.Request, status int, data []byte, mime, contentRange string, length int64) *http.Response {
	headers := make(http.Header)
	headers.Set("Content-Length", stringInt(length))
	headers.Set("Content-Type", mime)
	if contentRange != "" {
		headers.Set("Content-Range", contentRange)
	}
	return responseWithHeaders(req, status, data, headers)
}
