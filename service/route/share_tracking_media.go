package route

import (
	"crypto/rand"
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"service/tracking"
	"strings"
)

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
	root, e := trackingMediaRoot()
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
	switch mime {
	case "image/png", "image/jpeg", "image/gif":
	case "video/mp4":
		kind = "video"
		max = 50 << 20
	default:
		trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "仅支持经过类型校验的PNG/JPEG/GIF和MP4文件"})
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
	asset := tracking.Asset{ID: id, Kind: kind, URL: "/api/v1/miniprogram/tracking/media/" + id, Width: width, Height: height, MIME: mime, Title: filepath.Base(header.Filename)}
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
	root, e := trackingMediaRoot()
	if e != nil {
		trackingResponse(c, nil, e)
		return
	}
	f, e := os.Open(filepath.Join(root, id))
	if e != nil {
		trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "媒体不可访问"})
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		trackingResponse(c, nil, tracking.ErrUnavailable)
		return
	}
	c.Header("Content-Type", asset.MIME)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Security-Policy", "default-src 'none'")
	http.ServeContent(c.Writer, c.Request, asset.ID, info.ModTime(), f)
}
