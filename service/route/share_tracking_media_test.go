package route

import (
	"archive/zip"
	"bytes"
	"github.com/gin-gonic/gin"
	"mime/multipart"
	"net/http/httptest"
	"service/tracking"
	"testing"
)

func TestTrackingMediaRequiresExplicitAuthorizedOwner(t *testing.T) {
	t.Setenv("TRACKING_MEDIA_DIR", t.TempDir())
	gin.SetMode(gin.TestMode)
	for _, owner := range []string{"", "outside"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("rights", "own")
		writer.WriteField("owner_id", owner)
		writer.Close()
		router := gin.New()
		router.POST("/", func(c *gin.Context) {
			uploadTrackingMedia(c, nil, tracking.Scope{Manage: true, Owners: []string{"first", "second"}})
		})
		request := httptest.NewRequest("POST", "/", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("owner %q response %d", owner, response.Code)
		}
	}
}

// B4：PPTX 与 DOCX 同源校验——合法结构通过，宏载体拒绝。
func buildPPTX(t *testing.T, contentTypes string, extra map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := map[string]string{"[Content_Types].xml": contentTypes}
	for name, body := range extra {
		files[name] = body
	}
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestValidatePPTXAcceptsCleanSlidesAndRejectsMacros(t *testing.T) {
	cleanTypes := `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/></Types>`
	if !validatePPTX(buildPPTX(t, cleanTypes, map[string]string{"ppt/presentation.xml": "<presentation/>"})) {
		t.Fatal("合法 PPTX 应通过校验")
	}
	macroTypes := `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/><Override PartName="/ppt/vbaProject.bin" ContentType="application/vnd.ms-office.vbaproject"/></Types>`
	if validatePPTX(buildPPTX(t, macroTypes, map[string]string{"ppt/presentation.xml": "<presentation/>"})) {
		t.Fatal("含 VBA 声明的 PPTX 必须拒绝")
	}
	if validatePPTX(buildPPTX(t, cleanTypes, map[string]string{"ppt/vbaProject.bin": "macro"})) {
		t.Fatal("含 vbaProject.bin 的 PPTX 必须拒绝")
	}
	if validatePPTX(buildPPTX(t, `<?xml version="1.0"?><Types/>`, map[string]string{"ppt/presentation.xml": "<presentation/>"})) {
		t.Fatal("Content_Types 未声明 presentation 主部件必须拒绝")
	}
	if validatePPTX([]byte("not a zip")) {
		t.Fatal("非 zip 数据必须拒绝")
	}
}
