package route

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"service/tracking"
)

func documentUploadBody(t *testing.T, name string, data []byte, originalName ...string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("owner_id", "owner-a")
	_ = w.WriteField("rights", "own")
	if len(originalName) > 0 {
		_ = w.WriteField("original_name", originalName[0])
	}
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, w.FormDataContentType()
}

func docxBytes(t *testing.T, macroPath, macroMIME string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	contentType := `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`
	if macroMIME != "" {
		contentType = `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Default Extension="bin" ContentType="` + macroMIME + `"/></Types>`
	}
	for name, data := range map[string]string{"[Content_Types].xml": contentType, "word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`} {
		part, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if macroPath != "" {
		part, _ := w.Create(macroPath)
		_, _ = part.Write([]byte("macro"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestWorkbenchUploadsOnlyPDFAndMacroFreeDOCXDocuments(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	t.Setenv("CONTENT_MEDIA_STORAGE", "filesystem")
	mediaRoot := t.TempDir()
	if err := os.Chmod(mediaRoot, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACKING_MEDIA_DIR", mediaRoot)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r, _ := newWorkbenchHandlerFixture(t, &scope)
	for _, tc := range []struct {
		name         string
		data         []byte
		originalName string
		want         int
	}{
		{name: "tmp-file", originalName: "方案.pdf", data: []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"), want: http.StatusOK},
		{name: "tmp-file", originalName: "说明.docx", data: docxBytes(t, "", ""), want: http.StatusOK},
		{name: "tmp-file", originalName: "../越权.pdf", data: []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"), want: http.StatusBadRequest},
		{name: "不完整.pdf", data: []byte("%PDF-not-a-complete-document"), want: http.StatusBadRequest},
		{name: "伪装宏.docx", data: docxBytes(t, "word/vbaProject.bin", ""), want: http.StatusBadRequest},
		{name: "变体路径.docx", data: docxBytes(t, "custom/vbaProject.bin", ""), want: http.StatusBadRequest},
		{name: "MIME宏.docx", data: docxBytes(t, "", "application/vnd.ms-office.vbaProject"), want: http.StatusBadRequest},
		{name: "宏文档.docm", data: docxBytes(t, "word/vbaProject.bin", ""), want: http.StatusBadRequest},
		{name: "伪装.txt", data: []byte("plain text"), want: http.StatusBadRequest},
	} {
		body, contentType := documentUploadBody(t, tc.name, tc.data, tc.originalName)
		request := httptest.NewRequest(http.MethodPost, "/workbench/media", body)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Fatalf("%s = %d %s", tc.name, response.Code, response.Body.String())
		}
		if tc.want == http.StatusOK {
			var payload struct {
				Data tracking.Asset `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Data.Kind != "document" || (payload.Data.MIME != "application/pdf" && payload.Data.MIME != "application/vnd.openxmlformats-officedocument.wordprocessingml.document") {
				t.Fatalf("%s 返回了无效文档资产: %+v %v", tc.name, payload, err)
			}
			get := httptest.NewRecorder()
			r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/workbench/media/"+payload.Data.ID, nil))
			if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), tc.data) || !strings.Contains(get.Header().Get("Content-Disposition"), "attachment") {
				t.Fatalf("%s 原文件无法通过鉴权路由取回: %d %q", tc.name, get.Code, get.Header().Get("Content-Disposition"))
			}
		}
	}
}

func TestDocumentExpandedLimitCannotOverflow(t *testing.T) {
	if _, ok := addDocumentExpanded(documentExpandedMax-1, math.MaxUint64); ok {
		t.Fatal("极大 DOCX 条目绕过解压体积限制")
	}
}
