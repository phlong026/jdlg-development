package report

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

func TestCOSUploadRequiresMatchingCloudObject(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(strconv.FormatBool(mismatch), func(t *testing.T) {
			var digest string
			heads := 0
			pdf := []byte("%PDF-1.7\ntest")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write(pdf)
					return
				}
				if r.Method == http.MethodPut {
					digest = r.Header.Get("x-cos-meta-report-sha256")
					w.WriteHeader(http.StatusOK)
					return
				}
				if r.Method != http.MethodHead {
					t.Errorf("unexpected method %s", r.Method)
				}
				heads++
				w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
				if mismatch {
					digest = "wrong"
				}
				w.Header().Set("x-cos-meta-report-sha256", digest)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			client := cos.NewClient(&cos.BaseURL{BucketURL: u}, server.Client())
			client.Conf.EnableCRC = false
			storage, err := NewCOSPrivateStorage(client, "test-id", "test-key", "private", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = storage.UploadPrivate(context.Background(), "reports/11/test/full.pdf", pdf)
			if mismatch && !errors.Is(err, errStorageUnavailable) {
				t.Fatalf("mismatched cloud object accepted: %v", err)
			}
			if !mismatch && err != nil {
				t.Fatal(err)
			}
			if heads != 1 || len(digest) == 0 {
				t.Fatalf("cloud verification missing: heads=%d", heads)
			}
		})
	}
}

func TestCOSUploadRejectsCorruptBytesEvenWhenMetadataMatches(t *testing.T) {
	pdf := []byte("%PDF-1.7\noriginal")
	var digest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			digest = r.Header.Get("x-cos-meta-report-sha256")
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
			w.Header().Set("x-cos-meta-report-sha256", digest)
		case http.MethodGet:
			_, _ = w.Write([]byte("%PDF-1.7\nmodified"))
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, server.Client())
	client.Conf.EnableCRC = false
	storage, err := NewCOSPrivateStorage(client, "test-id", "test-key", "private", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.UploadPrivate(context.Background(), "reports/test/full.pdf", pdf); !errors.Is(err, errStorageUnavailable) {
		t.Fatalf("corrupt stored bytes were published: %v", err)
	}
}
