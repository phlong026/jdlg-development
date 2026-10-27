package contentstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/crc64"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAssetID = "0123456789abcdefghijklmnopqrstuv"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestNewCOSStoreDefaultsToUnavailable(t *testing.T) {
	store, err := NewCOSStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Head(context.Background(), "tenant", testAssetID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Head error = %v, want ErrUnavailable", err)
	}
}

func TestObjectKeyIsPrivateAndTenantScoped(t *testing.T) {
	store := newTestStore(t, nil, nil)
	keyA, err := store.ObjectKey("tenant-a", testAssetID)
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := store.ObjectKey("tenant-b", testAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if keyA == keyB || !strings.HasPrefix(keyA, DefaultPrivatePrefix+"/") {
		t.Fatalf("tenant-scoped keys not produced: %q %q", keyA, keyB)
	}
	if strings.Contains(keyA, "tenant-a") || strings.Contains(keyA, "ai_assets") {
		t.Fatalf("key leaks tenant or enters public prefix: %q", keyA)
	}
	parts := strings.Split(keyA, "/")
	if len(parts) != 3 || len(parts[1]) != 64 || parts[2] != testAssetID {
		t.Fatalf("unexpected key shape: %q", keyA)
	}
}

func TestCOSStoreRejectsUnsafeConfigurationAndIdentity(t *testing.T) {
	for _, prefix := range []string{"ai_assets", "private/ai_assets", "/private", "private/../asset"} {
		_, err := NewCOSStore(&Config{Enabled: true, Bucket: "bucket-1250000000", Region: "ap-guangzhou", PrivatePrefix: prefix, SecretID: "id", SecretKey: "key"})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("prefix %q error = %v, want ErrInvalidInput", prefix, err)
		}
	}
	store := newTestStore(t, nil, nil)
	for _, tc := range []struct{ tenant, assetID string }{
		{"", testAssetID},
		{" tenant", testAssetID},
		{"tenant", "short"},
		{"tenant", strings.Repeat("a", 31) + "+"},
	} {
		if _, err := store.ObjectKey(tc.tenant, tc.assetID); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ObjectKey(%q, %q) error = %v", tc.tenant, tc.assetID, err)
		}
	}
}

func TestPutSignsPrivateConditionalWriteAndProvesAnonymousDenied(t *testing.T) {
	data := []byte("private video")
	hash := sha256.Sum256(data)
	wantHash := hex.EncodeToString(hash[:])
	var mu sync.Mutex
	var methods []string
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		methods = append(methods, req.Method)
		mu.Unlock()
		if req.Header.Get("Authorization") == "" {
			t.Error("authenticated COS request is unsigned")
		}
		switch req.Method {
		case http.MethodHead:
			if len(methods) == 1 {
				return response(req, http.StatusNotFound, nil, ""), nil
			}
			return metadataResponse(req, data, "video/mp4", wantHash), nil
		case http.MethodPut:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != string(data) {
				t.Errorf("body = %q", body)
			}
			for name, want := range map[string]string{
				"x-cos-acl":              "private",
				"x-cos-meta-sha256":      wantHash,
				"If-None-Match":          "*",
				"x-cos-forbid-overwrite": "true",
				"Content-Type":           "video/mp4",
			} {
				if got := req.Header.Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			return successfulPutResponse(req, data), nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return nil, nil
		}
	})
	var anonymousCalls int
	anonymous := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		anonymousCalls++
		if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
			t.Fatalf("anonymous probe carried credentials: %#v", req.Header)
		}
		if req.URL.Scheme != "https" || req.URL.Host != "bucket-1250000000.cos.ap-guangzhou.myqcloud.com" {
			t.Fatalf("anonymous probe URL = %s", req.URL.Redacted())
		}
		if req.Method == http.MethodGet && req.Header.Get("Range") != "bytes=0-0" {
			t.Fatalf("anonymous GET Range = %q", req.Header.Get("Range"))
		}
		return response(req, http.StatusForbidden, nil, ""), nil
	})
	store := newTestStore(t, auth, anonymous)
	if err := store.Put(context.Background(), "tenant", testAssetID, data, "video/mp4"); err != nil {
		t.Fatalf("Put error = %v after methods %v", err, methods)
	}
	if got, want := strings.Join(methods, ","), "HEAD,PUT,HEAD"; got != want {
		t.Fatalf("methods = %s, want %s", got, want)
	}
	if anonymousCalls != 2 {
		t.Fatalf("anonymous calls = %d, want 2", anonymousCalls)
	}
}

func TestPutExistingIdenticalStillProvesPrivateWithoutWriting(t *testing.T) {
	data := []byte("same")
	hash := sha256.Sum256(data)
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodHead {
			t.Fatalf("unexpected write for identical object: %s", req.Method)
		}
		return metadataResponse(req, data, "image/png", hex.EncodeToString(hash[:])), nil
	})
	probed := false
	anonymous := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		probed = true
		return response(req, http.StatusNotFound, nil, ""), nil
	})
	store := newTestStore(t, auth, anonymous)
	if err := store.Put(context.Background(), "tenant", testAssetID, data, "image/png"); err != nil {
		t.Fatal(err)
	}
	if !probed {
		t.Fatal("existing object skipped anonymous privacy proof")
	}
}

func TestPutRefusesExistingConflict(t *testing.T) {
	existing := []byte("old")
	hash := sha256.Sum256(existing)
	store := newTestStore(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return metadataResponse(req, existing, "image/png", hex.EncodeToString(hash[:])), nil
	}), roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("conflicting existing object must not be probed as successful")
		return nil, nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, []byte("new"), "image/png"); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put error = %v, want ErrConflict", err)
	}
}

func TestPutRaceAcceptsOnlyIdenticalWinner(t *testing.T) {
	data := []byte("winner")
	hash := sha256.Sum256(data)
	calls := 0
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return response(req, http.StatusNotFound, nil, ""), nil
		case 2:
			return response(req, http.StatusPreconditionFailed, nil, ""), nil
		case 3:
			return metadataResponse(req, data, "video/mp4", hex.EncodeToString(hash[:])), nil
		default:
			t.Fatalf("unexpected call %d", calls)
			return nil, nil
		}
	})
	store := newTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusForbidden, nil, ""), nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, data, "video/mp4"); err != nil {
		t.Fatal(err)
	}
}

func TestPutRaceRejectsConflictingWinner(t *testing.T) {
	wanted := []byte("wanted")
	winner := []byte("other!")
	winnerHash := sha256.Sum256(winner)
	calls := 0
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return response(req, http.StatusNotFound, nil, ""), nil
		case 2:
			return response(req, http.StatusPreconditionFailed, nil, ""), nil
		case 3:
			return metadataResponse(req, winner, "video/mp4", hex.EncodeToString(winnerHash[:])), nil
		default:
			t.Fatalf("unexpected call %d", calls)
			return nil, nil
		}
	})
	store := newTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("conflicting race winner must not reach privacy success probe")
		return nil, nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, wanted, "video/mp4"); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put error = %v, want ErrConflict", err)
	}
}

func TestPutFailsClosedWhenObjectIsPublicOrProbeRedirects(t *testing.T) {
	data := []byte("secret")
	hash := sha256.Sum256(data)
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return metadataResponse(req, data, "image/jpeg", hex.EncodeToString(hash[:])), nil
	})
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"public", http.StatusOK, ErrPublicObject},
		{"redirect", http.StatusFound, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			anonymous := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				resp := response(req, tc.status, nil, "")
				if tc.status == http.StatusFound {
					resp.Header.Set("Location", "https://example.com/public")
				}
				return resp, nil
			})
			store := newTestStore(t, auth, anonymous)
			if err := store.Put(context.Background(), "tenant", testAssetID, data, "image/jpeg"); !errors.Is(err, tc.want) {
				t.Fatalf("Put error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPutClosesErrorAndPreconditionResponseBodies(t *testing.T) {
	data := []byte("sensitive")
	hash := sha256.Sum256(data)
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"ordinary error", http.StatusInternalServerError, ErrUnavailable},
		{"precondition", http.StatusPreconditionFailed, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			body := &trackingReadCloser{Reader: strings.NewReader("error")}
			auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				switch calls {
				case 1:
					return response(req, http.StatusNotFound, nil, ""), nil
				case 2:
					resp := response(req, tc.status, nil, "")
					resp.Body = body
					return resp, nil
				case 3:
					if tc.status != http.StatusPreconditionFailed {
						t.Fatalf("unexpected third call for %d", tc.status)
					}
					return metadataResponse(req, data, "video/mp4", hex.EncodeToString(hash[:])), nil
				default:
					t.Fatalf("unexpected call %d", calls)
					return nil, nil
				}
			})
			store := newTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return response(req, http.StatusForbidden, nil, ""), nil
			}))
			err := store.Put(context.Background(), "tenant", testAssetID, data, "video/mp4")
			if !errors.Is(err, tc.want) {
				t.Fatalf("Put error = %v, want %v", err, tc.want)
			}
			if !body.closed {
				t.Fatal("PUT error response body was not closed")
			}
		})
	}
}

func TestPublicCanaryPreventsUserDataUpload(t *testing.T) {
	userPuts := 0
	canaryDeletes := 0
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !isCanaryRequest(req) {
			if req.Method == http.MethodPut {
				userPuts++
			}
			t.Fatalf("user object request reached COS after public canary: %s %s", req.Method, req.URL.Path)
		}
		switch req.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(req.Body)
			return successfulPutResponse(req, body), nil
		case http.MethodHead:
			data := []byte(privateCanaryData)
			hash := sha256.Sum256(data)
			return metadataResponse(req, data, privateCanaryMIME, hex.EncodeToString(hash[:])), nil
		case http.MethodDelete:
			canaryDeletes++
			return response(req, http.StatusNoContent, nil, ""), nil
		default:
			t.Fatalf("unexpected canary method %s", req.Method)
			return nil, nil
		}
	})
	store := newRawTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !isCanaryRequest(req) {
			t.Fatal("user object reached anonymous probe after public canary")
		}
		return response(req, http.StatusOK, nil, ""), nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, []byte("real-user-bytes"), "video/mp4"); !errors.Is(err, ErrPublicObject) {
		t.Fatalf("Put error = %v, want ErrPublicObject", err)
	}
	if userPuts != 0 || canaryDeletes != 1 {
		t.Fatalf("user puts/canary deletes = %d/%d", userPuts, canaryDeletes)
	}
}

func TestAnonymousGETCanExposeCanaryEvenWhenHEADIsForbidden(t *testing.T) {
	userPuts := 0
	canaryDeletes := 0
	getBody := &trackingReadCloser{Reader: strings.NewReader("x")}
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !isCanaryRequest(req) {
			if req.Method == http.MethodPut {
				userPuts++
			}
			t.Fatalf("user object request reached COS after public canary GET: %s %s", req.Method, req.URL.Path)
		}
		switch req.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(req.Body)
			return successfulPutResponse(req, body), nil
		case http.MethodHead:
			data := []byte(privateCanaryData)
			hash := sha256.Sum256(data)
			return metadataResponse(req, data, privateCanaryMIME, hex.EncodeToString(hash[:])), nil
		case http.MethodDelete:
			canaryDeletes++
			return response(req, http.StatusNoContent, nil, ""), nil
		default:
			t.Fatalf("unexpected canary method %s", req.Method)
			return nil, nil
		}
	})
	anonymous := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
			t.Fatalf("anonymous probe carried credentials: %#v", req.Header)
		}
		switch req.Method {
		case http.MethodHead:
			return response(req, http.StatusForbidden, nil, ""), nil
		case http.MethodGet:
			if req.Header.Get("Range") != "bytes=0-0" {
				t.Fatalf("anonymous GET Range = %q", req.Header.Get("Range"))
			}
			resp := response(req, http.StatusPartialContent, nil, privateCanaryMIME)
			resp.Body = getBody
			return resp, nil
		default:
			t.Fatalf("unexpected anonymous method %s", req.Method)
			return nil, nil
		}
	})
	store := newRawTestStore(t, auth, anonymous)
	if err := store.Put(context.Background(), "tenant", testAssetID, []byte("real-user-bytes"), "video/mp4"); !errors.Is(err, ErrPublicObject) {
		t.Fatalf("Put error = %v, want ErrPublicObject", err)
	}
	if userPuts != 0 || canaryDeletes != 1 || !getBody.closed {
		t.Fatalf("user puts/canary deletes/get closed = %d/%d/%v", userPuts, canaryDeletes, getBody.closed)
	}
}

func TestPublicCanaryCleanupFailureReturnsUnavailable(t *testing.T) {
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !isCanaryRequest(req) {
			t.Fatalf("user object request reached COS: %s %s", req.Method, req.URL.Path)
		}
		switch req.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(req.Body)
			return successfulPutResponse(req, body), nil
		case http.MethodHead:
			data := []byte(privateCanaryData)
			hash := sha256.Sum256(data)
			return metadataResponse(req, data, privateCanaryMIME, hex.EncodeToString(hash[:])), nil
		case http.MethodDelete:
			return response(req, http.StatusInternalServerError, nil, ""), nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return nil, nil
		}
	})
	store := newRawTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, nil, ""), nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, []byte("real-user-bytes"), "video/mp4"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Put error = %v, want ErrUnavailable", err)
	}
}

func TestPostWritePublicObjectIsDeleted(t *testing.T) {
	data := []byte("sensitive")
	hash := sha256.Sum256(data)
	calls := 0
	deletedUserObject := false
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.Method {
		case http.MethodHead:
			if calls == 1 {
				return response(req, http.StatusNotFound, nil, ""), nil
			}
			return metadataResponse(req, data, "video/mp4", hex.EncodeToString(hash[:])), nil
		case http.MethodPut:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			return successfulPutResponse(req, body), nil
		case http.MethodDelete:
			deletedUserObject = true
			return response(req, http.StatusNoContent, nil, ""), nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return nil, nil
		}
	})
	store := newTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, nil, ""), nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, data, "video/mp4"); !errors.Is(err, ErrPublicObject) {
		t.Fatalf("Put error = %v, want ErrPublicObject", err)
	}
	if !deletedUserObject {
		t.Fatal("newly created public object was not deleted")
	}
}

func TestPostWritePublicCleanupFailureReturnsUnavailable(t *testing.T) {
	data := []byte("sensitive")
	hash := sha256.Sum256(data)
	calls := 0
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.Method {
		case http.MethodHead:
			if calls == 1 {
				return response(req, http.StatusNotFound, nil, ""), nil
			}
			return metadataResponse(req, data, "video/mp4", hex.EncodeToString(hash[:])), nil
		case http.MethodPut:
			body, _ := io.ReadAll(req.Body)
			return successfulPutResponse(req, body), nil
		case http.MethodDelete:
			return response(req, http.StatusInternalServerError, nil, ""), nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return nil, nil
		}
	})
	store := newTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, nil, ""), nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, data, "video/mp4"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Put error = %v, want ErrUnavailable", err)
	}
}

func TestExistingPublicObjectIsNeverDeleted(t *testing.T) {
	data := []byte("existing")
	hash := sha256.Sum256(data)
	deletedUserObject := false
	auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodHead:
			return metadataResponse(req, data, "video/mp4", hex.EncodeToString(hash[:])), nil
		case http.MethodDelete:
			deletedUserObject = true
			t.Fatal("existing user object must never be deleted")
			return nil, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return nil, nil
		}
	})
	store := newTestStore(t, auth, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, nil, ""), nil
	}))
	if err := store.Put(context.Background(), "tenant", testAssetID, data, "video/mp4"); !errors.Is(err, ErrPublicObject) {
		t.Fatalf("Put error = %v, want ErrPublicObject", err)
	}
	if deletedUserObject {
		t.Fatal("existing public object was deleted")
	}
}

func TestGetChecksBoundsLengthHashAndMIME(t *testing.T) {
	good := []byte("complete")
	goodHash := sha256.Sum256(good)
	for _, tc := range []struct {
		name     string
		headData []byte
		getData  []byte
		getMIME  string
		max      int64
		want     error
	}{
		{"success", good, good, "video/mp4", 100, nil},
		{"truncated", good, good[:3], "video/mp4", 100, ErrIntegrity},
		{"hash mismatch", good, []byte("tampered"), "video/mp4", 100, ErrIntegrity},
		{"mime mismatch", good, good, "text/plain", 100, ErrIntegrity},
		{"caller bound", good, good, "video/mp4", 3, ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			auth := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method == http.MethodHead {
					return metadataResponse(req, tc.headData, "video/mp4", hex.EncodeToString(goodHash[:])), nil
				}
				return response(req, http.StatusOK, tc.getData, tc.getMIME), nil
			})
			store := newTestStore(t, auth, nil)
			got, gotMIME, err := store.Get(context.Background(), "tenant", testAssetID, tc.max)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Get error = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (string(got) != string(good) || gotMIME != "video/mp4") {
				t.Fatalf("Get = %q, %q", got, gotMIME)
			}
			if errors.Is(tc.want, ErrInvalidInput) && calls != 1 {
				t.Fatalf("oversize request performed GET; calls = %d", calls)
			}
		})
	}
}

func TestHeadRejectsOversizeOrMissingHash(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
		hash string
	}{
		{"oversize", MaxObjectSize + 1, strings.Repeat("a", 64)},
		{"missing hash", 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				h := make(http.Header)
				h.Set("Content-Length", stringInt(tc.size))
				h.Set("Content-Type", "image/png")
				h.Set(sha256MetadataHeader, tc.hash)
				return responseWithHeaders(req, http.StatusOK, nil, h), nil
			}), nil)
			if _, err := store.Head(context.Background(), "tenant", testAssetID); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("Head error = %v, want ErrIntegrity", err)
			}
		})
	}
}

func TestClientsUseConfiguredTimeoutAndNoCookieJar(t *testing.T) {
	store, err := NewCOSStore(&Config{
		Enabled: true, Bucket: "bucket-1250000000", Region: "ap-guangzhou",
		SecretID: "id", SecretKey: "key", Timeout: 3 * time.Second,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unused") }),
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.anonymous.Timeout != 3*time.Second || store.anonymous.Jar != nil {
		t.Fatalf("anonymous client timeout/jar = %v/%v", store.anonymous.Timeout, store.anonymous.Jar)
	}
}

func TestUpstreamErrorsDoNotLeakCredentials(t *testing.T) {
	secret := "do-not-leak-secret"
	store := newTestStore(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(secret)
	}), nil)
	_, err := store.Head(context.Background(), "tenant", testAssetID)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), secret) {
		t.Fatalf("Head error = %q", err)
	}
}

func newTestStore(t *testing.T, auth, anonymous http.RoundTripper) *COSStore {
	t.Helper()
	if auth == nil {
		auth = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return response(req, http.StatusNotFound, nil, ""), nil
		})
	}
	if anonymous == nil {
		anonymous = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return response(req, http.StatusForbidden, nil, ""), nil
		})
	}
	auth = withSuccessfulCanaryAuth(t, auth)
	anonymous = withPrivateCanaryProbe(anonymous)
	return newRawTestStore(t, auth, anonymous)
}

func newRawTestStore(t *testing.T, auth, anonymous http.RoundTripper) *COSStore {
	t.Helper()
	store, err := NewCOSStore(&Config{
		Enabled: true, Bucket: "bucket-1250000000", Region: "ap-guangzhou",
		SecretID: "test-id", SecretKey: "test-key",
		Transport: auth, AnonymousTransport: anonymous,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func withSuccessfulCanaryAuth(t *testing.T, next http.RoundTripper) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !isCanaryRequest(req) {
			return next.RoundTrip(req)
		}
		switch req.Method {
		case http.MethodPut:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != privateCanaryData {
				t.Fatalf("canary body = %q", body)
			}
			return successfulPutResponse(req, body), nil
		case http.MethodHead:
			data := []byte(privateCanaryData)
			hash := sha256.Sum256(data)
			return metadataResponse(req, data, privateCanaryMIME, hex.EncodeToString(hash[:])), nil
		case http.MethodDelete:
			return response(req, http.StatusNoContent, nil, ""), nil
		default:
			t.Fatalf("unexpected canary method %s", req.Method)
			return nil, nil
		}
	})
}

func withPrivateCanaryProbe(next http.RoundTripper) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if isCanaryRequest(req) {
			return response(req, http.StatusForbidden, nil, ""), nil
		}
		return next.RoundTrip(req)
	})
}

func isCanaryRequest(req *http.Request) bool {
	return strings.Contains(req.URL.Path, "/_privacy-canary/")
}

func metadataResponse(req *http.Request, data []byte, contentType, hash string) *http.Response {
	headers := make(http.Header)
	headers.Set("Content-Length", stringInt(int64(len(data))))
	headers.Set("Content-Type", contentType)
	headers.Set(sha256MetadataHeader, hash)
	return responseWithHeaders(req, http.StatusOK, nil, headers)
}

func successfulPutResponse(req *http.Request, data []byte) *http.Response {
	resp := response(req, http.StatusOK, nil, "")
	resp.Header.Set("x-cos-hash-crc64ecma", strconv.FormatUint(crc64.Checksum(data, crc64.MakeTable(crc64.ECMA)), 10))
	return resp
}

func response(req *http.Request, status int, data []byte, contentType string) *http.Response {
	headers := make(http.Header)
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	if data != nil {
		headers.Set("Content-Length", stringInt(int64(len(data))))
	}
	return responseWithHeaders(req, status, data, headers)
}

func responseWithHeaders(req *http.Request, status int, data []byte, headers http.Header) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(string(data))),
		Request:    req,
	}
}

func stringInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
