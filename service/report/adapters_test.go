package report

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

func TestHTTPTextClientReturnsOnlyBoundedExplanationFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing authorization")
		}
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{"choices":[{"message":{"content":"{\"profile_insight\":\"家庭画像解释\",\"core_tradeoff\":\"核心取舍解释\",\"recommendation\":\"下一步建议\"}"}}]}`)
	}))
	defer server.Close()
	client, err := NewHTTPTextClient(server.URL, "test-key", "test-model", time.Second, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Generate(context.Background(), TextRequest{Facts: SnapshotFacts{Region: "福建"}})
	if err != nil || result.CoreTradeoff != "核心取舍解释" {
		t.Fatalf("Generate() = %#v, %v", result, err)
	}
}

func TestCOSStorageUploadsWithPrivateACLAndReturnsSignedURL(t *testing.T) {
	var privateACL bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		privateACL = request.Header.Get("x-cos-acl") == "private" && request.Header.Get("Content-Type") == "application/pdf"
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	bucketURL, _ := url.Parse(server.URL)
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, server.Client())
	client.Conf.EnableCRC = false
	storage, err := NewCOSPrivateStorage(client, "secret-id", "secret-key", "private", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	key := "reports/11/R-1/full-lease.pdf"
	if err := storage.UploadPrivate(context.Background(), key, []byte("%PDF-1.7\ntest")); err != nil {
		t.Fatal(err)
	}
	if !privateACL {
		t.Fatal("COS upload was not explicitly private")
	}
	signed, err := storage.SignDownload(context.Background(), key, time.Minute)
	if err != nil || !strings.Contains(signed, "q-signature=") || signed == server.URL+"/private/"+key {
		t.Fatalf("signed URL = %q, %v", signed, err)
	}
}
