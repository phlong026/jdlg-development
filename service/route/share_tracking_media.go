package route

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"service/tracking"
	"strings"
)

func addDocumentExpanded(total, size uint64) (uint64, bool) {
	if total > documentExpandedMax || size > documentExpandedMax-total {
		return total, false
	}
	return total + size, true
}

func validatePDF(data []byte) bool {
	if len(data) < 16 || !bytes.HasPrefix(data, []byte("%PDF-")) || (data[5] != '1' && data[5] != '2') || data[6] != '.' || data[7] < '0' || data[7] > '9' {
		return false
	}
	trimmed := bytes.TrimSpace(data)
	return bytes.HasSuffix(trimmed, []byte("%%EOF")) && bytes.Contains(data, []byte(" obj")) && bytes.Contains(data, []byte("endobj"))
}

const (
	documentMaxBytes       = int64(20 << 20)
	documentExpandedMax    = uint64(100 << 20)
	documentContentTypeMax = int64(1 << 20)
)

func validateDOCX(data []byte) bool {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > 5000 {
		return false
	}
	foundTypes, foundDocument := false, false
	var expanded uint64
	for _, file := range reader.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if strings.HasPrefix(name, "/") || strings.Contains(name, "../") {
			return false
		}
		var ok bool
		expanded, ok = addDocumentExpanded(expanded, file.UncompressedSize64)
		if !ok {
			return false
		}
		if strings.EqualFold(path.Base(name), "vbaProject.bin") {
			return false
		}
		switch name {
		case "word/document.xml":
			foundDocument = true
		case "[Content_Types].xml":
			if file.UncompressedSize64 > uint64(documentContentTypeMax) {
				return false
			}
			input, err := file.Open()
			if err != nil {
				return false
			}
			content, err := io.ReadAll(io.LimitReader(input, documentContentTypeMax+1))
			closeErr := input.Close()
			lower := bytes.ToLower(content)
			if err != nil || closeErr != nil || int64(len(content)) > documentContentTypeMax || bytes.Contains(lower, []byte("macroenabled")) || bytes.Contains(lower, []byte("application/vnd.ms-office.vbaproject")) {
				return false
			}
			foundTypes = bytes.Contains(content, []byte("application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"))
		}
	}
	return foundTypes && foundDocument
}

func trackingMediaRoot() (string, error) {
	root := os.Getenv("TRACKING_MEDIA_DIR")
	if root == "" || !filepath.IsAbs(root) {
		return "", tracking.ErrUnavailable
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return "", e
	}
	return root, nil
}
func uploadTrackingMedia(c *gin.Context, s *tracking.Service, scope tracking.Scope) {
	if !scope.Manage || len(scope.Owners) == 0 {
		trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "无媒体上传权限"})
		return
	}
	if os.Getenv("CONTENT_MEDIA_STORAGE") == "cos" && (s == nil || s.MediaObjects == nil) {
		trackingResponse(c, nil, tracking.ErrUnavailable)
		return
	}
	root := ""
	var e error
	if s == nil || s.MediaObjects == nil {
		root, e = trackingMediaRoot()
	}
	if e != nil {
		trackingResponse(c, nil, e)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
	if e = c.Request.ParseMultipartForm(1 << 20); e != nil {
		trackingResponse(c, nil, &tracking.Error{Status: 413, Message: "上传体积超过限制"})
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	owner := c.PostForm("owner_id")
	if !scope.Allows(owner) {
		trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "请选择已授权媒体归属"})
		return
	}
	rights := c.PostForm("rights")
	if rights != "own" && rights != "authorized" {
		trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "请声明媒体权属"})
		return
	}
	source, header, e := c.Request.FormFile("file")
	if e != nil {
		trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "缺少媒体文件"})
		return
	}
	defer source.Close()
	sniff := make([]byte, 512)
	n, _ := io.ReadFull(source, sniff)
	sniff = sniff[:n]
	mime := http.DetectContentType(sniff)
	kind := "image"
	max := int64(10 << 20)
	filename := filepath.Base(header.Filename)
	if original := strings.TrimSpace(c.PostForm("original_name")); original != "" {
		normalized := strings.ReplaceAll(original, "\\", "/")
		declared := path.Base(normalized)
		if len(original) > 200 || declared == "." || declared == "/" || declared != normalized {
			trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "原文件名声明无效"})
			return
		}
		filename = declared
	}
	extension := strings.ToLower(filepath.Ext(filename))
	switch {
	case extension == ".pdf" && mime == "application/pdf":
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			trackingResponse(c, nil, err)
			return
		}
		data, err := io.ReadAll(io.LimitReader(source, documentMaxBytes+1))
		if err != nil || int64(len(data)) > documentMaxBytes {
			trackingResponse(c, nil, &tracking.Error{Status: 413, Message: "PDF文档体积超过限制"})
			return
		}
		if !validatePDF(data) {
			trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "PDF基本结构或尾标无效"})
			return
		}
		kind = "document"
		max = documentMaxBytes
		_, _ = source.Seek(0, io.SeekStart)
	case extension == ".docx" && (mime == "application/zip" || mime == "application/octet-stream"):
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			trackingResponse(c, nil, err)
			return
		}
		data, err := io.ReadAll(io.LimitReader(source, documentMaxBytes+1))
		if err != nil || int64(len(data)) > documentMaxBytes {
			trackingResponse(c, nil, &tracking.Error{Status: 413, Message: "DOCX文档体积超过限制"})
			return
		}
		if !validateDOCX(data) {
			trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "DOCX结构无效或包含宏"})
			return
		}
		kind = "document"
		mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		max = documentMaxBytes
		_, _ = source.Seek(0, io.SeekStart)
	case mime == "image/png" || mime == "image/jpeg" || mime == "image/gif" || mime == "image/webp":
	case mime == "video/mp4":
		kind = "video"
		max = 50 << 20
	default:
		trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "仅支持经过类型校验的PNG/JPEG/GIF/WebP、MP4、PDF和无宏DOCX文件"})
		return
	}
	if header.Size > max {
		trackingResponse(c, nil, &tracking.Error{Status: 413, Message: "媒体体积超过限制"})
		return
	}
	if _, e = source.Seek(0, io.SeekStart); e != nil {
		trackingResponse(c, nil, e)
		return
	}
	width, height := 0, 0
	if kind == "image" {
		config, _, err := image.DecodeConfig(source)
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 20000 || config.Height > 20000 {
			trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "图片尺寸无效"})
			return
		}
		width, height = config.Width, config.Height
		_, _ = source.Seek(0, io.SeekStart)
	}
	if kind == "video" {
		data, err := io.ReadAll(io.LimitReader(source, (50<<20)+1))
		if err != nil || len(data) > 50<<20 {
			trackingResponse(c, nil, &tracking.Error{Status: 413, Message: "视频超过限制"})
			return
		}
		width, height, err = trackingMP4Dimensions(data)
		if err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "无法识别MP4尺寸，请选择标准MP4原文件"})
			return
		}
		_, _ = source.Seek(0, io.SeekStart)
	}
	bytes := make([]byte, 24)
	if _, e = rand.Read(bytes); e != nil {
		trackingResponse(c, nil, e)
		return
	}
	id := base64.RawURLEncoding.EncodeToString(bytes)
	if s.MediaObjects != nil {
		data, err := io.ReadAll(io.LimitReader(source, max+1))
		if err != nil || int64(len(data)) > max {
			trackingResponse(c, nil, &tracking.Error{Status: 413, Message: "上传失败或超出限制"})
			return
		}
		asset := tracking.Asset{ID: id, Kind: kind, URL: "/api/v1/miniprogram/tracking/media/" + id, Width: width, Height: height, MIME: mime, Title: filename}
		if len(asset.Title) > 200 {
			asset.Title = "已上传媒体"
		}
		digest := sha256.Sum256(data)
		operation, _, err := s.PrepareMediaObject(c.Request.Context(), scope, asset, owner, fmt.Sprintf("%x", digest), "upload")
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		if err = storeContentMedia(c.Request.Context(), s, id, data, mime); err != nil {
			importResponse(c, nil, err)
			return
		}
		saved, err := s.FinalizeMediaObject(c.Request.Context(), scope, id, operation)
		trackingResponse(c, saved, err)
		return
	}
	file, e := os.OpenFile(filepath.Join(root, id), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		trackingResponse(c, nil, e)
		return
	}
	size, copyErr := io.Copy(file, io.LimitReader(source, max+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || size > max {
		_ = os.Remove(filepath.Join(root, id))
		trackingResponse(c, nil, &tracking.Error{Status: 413, Message: "上传失败或超出限制"})
		return
	}
	asset := tracking.Asset{ID: id, Kind: kind, URL: "/api/v1/miniprogram/tracking/media/" + id, Width: width, Height: height, MIME: mime, Title: filename}
	if len(asset.Title) > 200 {
		asset.Title = "已上传媒体"
	}
	e = s.AddMedia(c.Request.Context(), scope, asset, owner)
	if e != nil {
		_ = os.Remove(filepath.Join(root, id))
	}
	trackingResponse(c, asset, e)
}
func serveTrackingMedia(c *gin.Context, s *tracking.Service, scope *tracking.Scope) {
	id := c.Param("id")
	if len(id) != 32 || strings.ContainsAny(id, "/\\.") {
		trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "媒体不可访问"})
		return
	}
	asset, e := s.Media(c.Request.Context(), scope, id)
	if e != nil {
		trackingResponse(c, nil, e)
		return
	}
	serveContentMediaBytes(c, s, asset)
}
