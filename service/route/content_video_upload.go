package route

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"service/contentstorage"
	"service/tracking"
)

const (
	videoUploadChunkSize = int64(4 << 20)
	videoUploadTTL       = 7 * 24 * time.Hour
	videoUploadDoneTTL   = 30 * 24 * time.Hour
	videoUploadLockCount = 256
	trackingMP4MaxAtoms  = 10000
)

var videoUploadLocks [videoUploadLockCount]sync.Mutex
var videoUploadWorkers sync.Map
var videoUploadLastCleanup atomic.Int64

type videoUploadBegin struct {
	OwnerID     string `json:"owner_id"`
	Rights      string `json:"rights"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	Fingerprint string `json:"fingerprint"`
	Reset       bool   `json:"reset,omitempty"`
}

type videoUploadSession struct {
	UploadID    string            `json:"upload_id"`
	AssetID     string            `json:"asset_id"`
	Tenant      string            `json:"tenant"`
	Actor       int64             `json:"actor"`
	OwnerID     string            `json:"owner_id"`
	Rights      string            `json:"rights"`
	Filename    string            `json:"filename"`
	Size        int64             `json:"size"`
	Fingerprint string            `json:"fingerprint"`
	ChunkSize   int64             `json:"chunk_size"`
	Parts       map[string]string `json:"parts"`
	Asset       *tracking.Asset   `json:"asset,omitempty"`
	Processing  bool              `json:"processing,omitempty"`
	Error       string            `json:"error,omitempty"`
	Retryable   bool              `json:"retryable,omitempty"`
	ResetNeeded bool              `json:"reset_required,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type videoUploadStatus struct {
	UploadID      string          `json:"upload_id"`
	ChunkSize     int64           `json:"chunk_size"`
	UploadedParts []int           `json:"uploaded_parts"`
	Asset         *tracking.Asset `json:"asset,omitempty"`
	Processing    bool            `json:"processing"`
	Error         string          `json:"error,omitempty"`
	Retryable     bool            `json:"retryable"`
	ResetRequired bool            `json:"reset_required"`
}

func registerVideoUploadHandlers(g *gin.RouterGroup, svc *tracking.Service, scope func(*gin.Context) tracking.Scope) {
	g.POST("/media-uploads", func(c *gin.Context) {
		var input videoUploadBegin
		if !trackingBind(c, &input) {
			return
		}
		status, err := beginVideoUpload(c.Request.Context(), svc, scope(c), input)
		trackingResponse(c, status, err)
	})
	g.GET("/media-uploads/:id", func(c *gin.Context) {
		status, err := videoUploadState(scope(c), c.Param("id"))
		if err == nil && status.Processing && status.Asset == nil {
			startVideoUploadWorker(svc, scope(c), c.Param("id"))
		}
		trackingResponse(c, status, err)
	})
	g.POST("/media-uploads/:id/parts/:part", func(c *gin.Context) {
		part, err := strconv.Atoi(c.Param("part"))
		if err != nil || part < 0 {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "分片编号无效"})
			return
		}
		status, err := uploadVideoPart(c, scope(c), c.Param("id"), part)
		trackingResponse(c, status, err)
	})
	g.POST("/media-uploads/:id/complete", func(c *gin.Context) {
		status, err := requestVideoUploadCompletion(svc, scope(c), c.Param("id"))
		trackingResponse(c, status, err)
	})
}

func videoUploadRoot() (string, error) {
	root, err := videoUploadRootPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return "", err
	}
	return root, nil
}

func videoUploadRootPath() (string, error) {
	root := strings.TrimSpace(os.Getenv("CONTENT_VIDEO_UPLOAD_DIR"))
	if root == "" {
		mediaRoot := strings.TrimSpace(os.Getenv("TRACKING_MEDIA_DIR"))
		if filepath.IsAbs(mediaRoot) {
			root = filepath.Join(mediaRoot, ".video-uploads")
		}
	}
	if !filepath.IsAbs(root) {
		return "", contentstorage.ErrUnavailable
	}
	return root, nil
}

func videoUploadConfigured() bool {
	root, err := videoUploadRootPath()
	if err != nil {
		return false
	}
	info, err := os.Stat(root)
	if err == nil {
		return info.IsDir() && info.Mode().Perm()&0077 == 0
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false
	}
	if strings.TrimSpace(os.Getenv("CONTENT_VIDEO_UPLOAD_DIR")) != "" {
		return true
	}
	parent, err := os.Stat(filepath.Dir(root))
	return err == nil && parent.IsDir() && parent.Mode().Perm()&0077 == 0
}

func beginVideoUpload(ctx context.Context, svc *tracking.Service, scope tracking.Scope, input videoUploadBegin) (videoUploadStatus, error) {
	if !scope.Manage || !scope.Allows(input.OwnerID) {
		return videoUploadStatus{}, &tracking.Error{Status: http.StatusForbidden, Message: "请选择已授权媒体归属"}
	}
	input.Filename = strings.TrimSpace(input.Filename)
	input.Fingerprint = strings.ToLower(strings.TrimSpace(input.Fingerprint))
	if safeVideoFilename(input.Filename) && !strings.EqualFold(filepath.Ext(input.Filename), ".mp4") {
		return videoUploadStatus{}, &tracking.Error{Status: http.StatusBadRequest, Message: "当前仅支持可直接播放的MP4，MOV请先转为MP4"}
	}
	if (input.Rights != "own" && input.Rights != "authorized") || input.Size <= 0 || !validVideoFingerprint(input.Fingerprint) || !validVideoFilename(input.Filename) {
		return videoUploadStatus{}, &tracking.Error{Status: http.StatusBadRequest, Message: "视频上传信息无效"}
	}
	root, err := videoUploadRoot()
	if err != nil {
		return videoUploadStatus{}, err
	}
	key := fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s\x00%d", scope.Tenant, scope.Actor, input.OwnerID, input.Rights, input.Fingerprint, input.Size)
	digest := sha256.Sum256([]byte(key))
	uploadID := hex.EncodeToString(digest[:16])
	assetID := base64.RawURLEncoding.EncodeToString(digest[8:32])
	now := time.Now().UTC()

	cleanupExpiredVideoUploads(root, now)
	lock := videoUploadSessionLock(root, uploadID)
	lock.Lock()
	defer lock.Unlock()
	if existing, loadErr := loadVideoUpload(root, uploadID); loadErr == nil {
		if authErr := authorizeVideoUpload(scope, existing); authErr != nil {
			return videoUploadStatus{}, authErr
		}
		if input.Reset && existing.Asset == nil {
			if !existing.ResetNeeded {
				return videoUploadStatus{}, &tracking.Error{Status: http.StatusConflict, Message: "当前上传会话无需重置"}
			}
			if err := os.RemoveAll(filepath.Join(root, uploadID)); err != nil {
				log.Printf("[video-upload] error upload=%s operation=reset_cleanup error_type=%T", uploadID, err)
				return videoUploadStatus{}, err
			}
		} else {
			return existing.status(), nil
		}
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return videoUploadStatus{}, loadErr
	}
	if svc != nil {
		if asset, mediaErr := svc.Media(ctx, &scope, assetID); mediaErr == nil {
			session := videoUploadSession{
				UploadID: uploadID, AssetID: assetID, Tenant: scope.Tenant, Actor: scope.Actor,
				OwnerID: input.OwnerID, Rights: input.Rights, Filename: input.Filename,
				Size: input.Size, Fingerprint: input.Fingerprint, ChunkSize: videoUploadChunkSize,
				Parts: map[string]string{}, Asset: &asset, CreatedAt: now, UpdatedAt: now,
			}
			if err = saveVideoUpload(root, session); err != nil {
				return videoUploadStatus{}, err
			}
			return session.status(), nil
		}
	}
	session := videoUploadSession{
		UploadID: uploadID, AssetID: assetID, Tenant: scope.Tenant, Actor: scope.Actor,
		OwnerID: input.OwnerID, Rights: input.Rights, Filename: input.Filename,
		Size: input.Size, Fingerprint: input.Fingerprint, ChunkSize: videoUploadChunkSize,
		Parts: map[string]string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err = saveVideoUpload(root, session); err != nil {
		return videoUploadStatus{}, err
	}
	return session.status(), nil
}

func videoUploadState(scope tracking.Scope, uploadID string) (videoUploadStatus, error) {
	root, err := videoUploadRoot()
	if err != nil {
		return videoUploadStatus{}, err
	}
	lock := videoUploadSessionLock(root, uploadID)
	lock.Lock()
	defer lock.Unlock()
	session, err := loadVideoUpload(root, uploadID)
	if err != nil {
		return videoUploadStatus{}, videoUploadLoadError(err)
	}
	if err = authorizeVideoUpload(scope, session); err != nil {
		return videoUploadStatus{}, err
	}
	return session.status(), nil
}

func uploadVideoPart(c *gin.Context, scope tracking.Scope, uploadID string, part int) (videoUploadStatus, error) {
	root, err := videoUploadRoot()
	if err != nil {
		return videoUploadStatus{}, err
	}
	lock := videoUploadSessionLock(root, uploadID)
	lock.Lock()
	defer lock.Unlock()
	session, err := loadVideoUpload(root, uploadID)
	if err != nil {
		return videoUploadStatus{}, videoUploadLoadError(err)
	}
	if err = authorizeVideoUpload(scope, session); err != nil {
		return videoUploadStatus{}, err
	}
	if session.Asset != nil {
		return session.status(), nil
	}
	if session.Processing {
		return videoUploadStatus{}, &tracking.Error{Status: http.StatusConflict, Message: "视频正在合并，请稍后查看"}
	}
	if session.ResetNeeded {
		return session.status(), nil
	}
	expected, ok := session.expectedPartSize(part)
	if !ok {
		return videoUploadStatus{}, &tracking.Error{Status: http.StatusBadRequest, Message: "分片编号超出视频范围"}
	}
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return videoUploadStatus{}, &tracking.Error{Status: http.StatusBadRequest, Message: "分片格式无效"}
	}
	dir := filepath.Join(root, session.UploadID)
	temp, err := os.CreateTemp(dir, ".part-")
	if err != nil {
		return videoUploadStatus{}, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	hasher := sha256.New()
	var copied int64
	found := false
	for {
		field, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			_ = temp.Close()
			return videoUploadStatus{}, &tracking.Error{Status: http.StatusBadRequest, Message: "分片格式无效"}
		}
		if field.FormName() != "file" || found {
			_ = field.Close()
			continue
		}
		found = true
		copied, err = io.CopyBuffer(io.MultiWriter(temp, hasher), io.LimitReader(field, expected+1), make([]byte, 32<<10))
		closeErr := field.Close()
		if err != nil || closeErr != nil {
			_ = temp.Close()
			return videoUploadStatus{}, contentstorage.ErrUnavailable
		}
	}
	if closeErr := temp.Close(); closeErr != nil {
		return videoUploadStatus{}, closeErr
	}
	if !found || copied != expected {
		return videoUploadStatus{}, &tracking.Error{Status: http.StatusBadRequest, Message: "分片字节数与声明不一致"}
	}
	partName := videoUploadPartPath(dir, part)
	if err = os.Rename(tempName, partName); err != nil {
		return videoUploadStatus{}, err
	}
	session.Parts[strconv.Itoa(part)] = hex.EncodeToString(hasher.Sum(nil))
	session.UpdatedAt = time.Now().UTC()
	if err = saveVideoUpload(root, session); err != nil {
		return videoUploadStatus{}, err
	}
	return session.status(), nil
}

func requestVideoUploadCompletion(svc *tracking.Service, scope tracking.Scope, uploadID string) (videoUploadStatus, error) {
	root, err := videoUploadRoot()
	if err != nil {
		return videoUploadStatus{}, err
	}
	lock := videoUploadSessionLock(root, uploadID)
	lock.Lock()
	session, err := loadVideoUpload(root, uploadID)
	if err != nil {
		lock.Unlock()
		return videoUploadStatus{}, videoUploadLoadError(err)
	}
	if err = authorizeVideoUpload(scope, session); err != nil {
		lock.Unlock()
		return videoUploadStatus{}, err
	}
	if session.Asset != nil {
		lock.Unlock()
		return session.status(), nil
	}
	if session.ResetNeeded {
		lock.Unlock()
		return session.status(), nil
	}
	for part := 0; ; part++ {
		if _, ok := session.expectedPartSize(part); !ok {
			break
		}
		if session.Parts[strconv.Itoa(part)] == "" {
			lock.Unlock()
			return videoUploadStatus{}, &tracking.Error{Status: http.StatusConflict, Message: "视频分片尚未上传完整"}
		}
	}
	session.Processing = true
	session.Error = ""
	session.Retryable = false
	session.UpdatedAt = time.Now().UTC()
	if err = saveVideoUpload(root, session); err != nil {
		lock.Unlock()
		return videoUploadStatus{}, err
	}
	status := session.status()
	lock.Unlock()
	startVideoUploadWorker(svc, scope, uploadID)
	return status, nil
}

func startVideoUploadWorker(svc *tracking.Service, scope tracking.Scope, uploadID string) {
	root, err := videoUploadRoot()
	if err != nil {
		return
	}
	key := root + ":" + uploadID
	if _, running := videoUploadWorkers.LoadOrStore(key, struct{}{}); running {
		return
	}
	scope.Owners = append([]string(nil), scope.Owners...)
	go func() {
		defer videoUploadWorkers.Delete(key)
		if _, err := finalizeVideoUpload(context.Background(), svc, scope, uploadID); err != nil {
			log.Printf("[video-upload] error upload=%s operation=finalize error_type=%T", uploadID, err)
			lock := videoUploadSessionLock(root, uploadID)
			lock.Lock()
			defer lock.Unlock()
			session, loadErr := loadVideoUpload(root, uploadID)
			if loadErr == nil && session.Asset == nil {
				retryable, reset := videoUploadFailure(err)
				session.Processing = false
				session.Error = videoUploadPublicError(err)
				session.Retryable = retryable
				session.ResetNeeded = reset
				session.UpdatedAt = time.Now().UTC()
				if saveErr := saveVideoUpload(root, session); saveErr != nil {
					log.Printf("[video-upload] error upload=%s operation=save_failure_status error_type=%T", uploadID, saveErr)
				}
			}
		}
	}()
}

func finalizeVideoUpload(ctx context.Context, svc *tracking.Service, scope tracking.Scope, uploadID string) (tracking.Asset, error) {
	root, err := videoUploadRoot()
	if err != nil {
		return tracking.Asset{}, err
	}
	lock := videoUploadSessionLock(root, uploadID)
	lock.Lock()
	session, err := loadVideoUpload(root, uploadID)
	lock.Unlock()
	if err != nil {
		return tracking.Asset{}, videoUploadLoadError(err)
	}
	if err = authorizeVideoUpload(scope, session); err != nil {
		return tracking.Asset{}, err
	}
	if session.Asset != nil {
		return *session.Asset, nil
	}
	dir := filepath.Join(root, session.UploadID)
	combined, err := os.CreateTemp(dir, ".complete-")
	if err != nil {
		return tracking.Asset{}, err
	}
	combinedPath := combined.Name()
	defer os.Remove(combinedPath)
	hasher := sha256.New()
	fingerprint := sha1.New()
	for part := 0; ; part++ {
		expected, ok := session.expectedPartSize(part)
		if !ok {
			break
		}
		if session.Parts[strconv.Itoa(part)] == "" {
			_ = combined.Close()
			return tracking.Asset{}, &tracking.Error{Status: http.StatusConflict, Message: "视频分片尚未上传完整"}
		}
		if err = appendVideoPart(combined, io.MultiWriter(hasher, fingerprint), videoUploadPartPath(dir, part), expected, session.Parts[strconv.Itoa(part)]); err != nil {
			_ = combined.Close()
			return tracking.Asset{}, err
		}
	}
	if err = combined.Close(); err != nil {
		return tracking.Asset{}, err
	}
	if hex.EncodeToString(fingerprint.Sum(nil)) != session.Fingerprint {
		return tracking.Asset{}, &tracking.Error{Status: http.StatusConflict, Message: "视频指纹校验失败，请重新选择原文件"}
	}
	file, err := os.Open(combinedPath)
	if err != nil {
		return tracking.Asset{}, err
	}
	width, height, err := trackingMP4DimensionsAtContext(ctx, file, session.Size)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return tracking.Asset{}, &tracking.Error{Status: http.StatusBadRequest, Message: "无法识别MP4尺寸，请选择标准MP4原文件"}
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	asset := tracking.Asset{ID: session.AssetID, Kind: "video", URL: "/api/v1/miniprogram/tracking/media/" + session.AssetID, Width: width, Height: height, MIME: "video/mp4", Title: session.Filename}
	if len([]rune(asset.Title)) > 200 {
		asset.Title = "已上传视频"
	}
	operation, _, err := svc.PrepareMediaObject(ctx, scope, asset, session.OwnerID, digest, "upload:resumable")
	if err != nil {
		return tracking.Asset{}, err
	}
	if err = storeContentMediaFile(ctx, svc, asset.ID, combinedPath, session.Size, asset.MIME, digest); err != nil {
		return tracking.Asset{}, err
	}
	saved, err := svc.FinalizeMediaObject(ctx, scope, asset.ID, operation)
	if err != nil {
		return tracking.Asset{}, err
	}
	session.Asset = &saved
	session.Processing = false
	session.Error = ""
	session.Retryable = false
	session.ResetNeeded = false
	session.UpdatedAt = time.Now().UTC()
	lock.Lock()
	defer lock.Unlock()
	if err = saveVideoUpload(root, session); err != nil {
		return tracking.Asset{}, err
	}
	for part := range session.Parts {
		index, _ := strconv.Atoi(part)
		_ = os.Remove(videoUploadPartPath(dir, index))
	}
	return saved, nil
}

func appendVideoPart(destination io.Writer, digest io.Writer, path string, expected int64, wantHash string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != expected {
		return &tracking.Error{Status: http.StatusConflict, Message: "视频分片文件不完整"}
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.CopyBuffer(io.MultiWriter(destination, digest, hasher), file, make([]byte, 32<<10))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != expected || hex.EncodeToString(hasher.Sum(nil)) != wantHash {
		return &tracking.Error{Status: http.StatusConflict, Message: "视频分片校验失败"}
	}
	return nil
}

func storeContentMediaFile(ctx context.Context, svc *tracking.Service, id, sourcePath string, size int64, mime, digest string) error {
	if !validStoredMediaID(id) || size <= 0 {
		return contentstorage.ErrInvalidInput
	}
	if os.Getenv("CONTENT_MEDIA_STORAGE") == "cos" {
		if svc == nil || svc.MediaObjects == nil {
			return contentstorage.ErrUnavailable
		}
		store, ok := svc.MediaObjects.(interface {
			PutFile(context.Context, string, string, string, int64, string, string) error
		})
		if !ok {
			return contentstorage.ErrUnavailable
		}
		return store.PutFile(ctx, svc.Config.Tenant, id, sourcePath, size, mime, digest)
	}
	root := os.Getenv("TRACKING_MEDIA_DIR")
	if !filepath.IsAbs(root) {
		return contentstorage.ErrUnavailable
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(root, ".video-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	written, copyErr := io.CopyBuffer(temp, source, make([]byte, 32<<10))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil || written != size {
		return contentstorage.ErrIntegrity
	}
	destination := filepath.Join(root, id)
	if err = os.Link(tempPath, destination); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	return verifyContentMediaFile(destination, size, digest)
}

func verifyContentMediaFile(path string, size int64, digest string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.CopyBuffer(hasher, file, make([]byte, 32<<10))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != size || hex.EncodeToString(hasher.Sum(nil)) != digest {
		return contentstorage.ErrConflict
	}
	return nil
}

func trackingMP4DimensionsAt(reader io.ReaderAt, size int64) (int, int, error) {
	return trackingMP4DimensionsAtContext(context.Background(), reader, size)
}

func trackingMP4DimensionsAtContext(ctx context.Context, reader io.ReaderAt, size int64) (int, int, error) {
	if size < 16 {
		return 0, 0, errors.New("short mp4")
	}
	var foundFTYP bool
	var width, height int
	atoms := 0
	var walk func(int64, int64, int) error
	walk = func(offset, length int64, depth int) error {
		if depth > 4 {
			return errors.New("mp4 nesting too deep")
		}
		end := offset + length
		for offset+8 <= end {
			if err := ctx.Err(); err != nil {
				return err
			}
			atoms++
			if atoms > trackingMP4MaxAtoms {
				return errors.New("too many mp4 atoms")
			}
			var header [16]byte
			if _, err := reader.ReadAt(header[:8], offset); err != nil {
				return err
			}
			boxSize := int64(binary.BigEndian.Uint32(header[:4]))
			kind := string(header[4:8])
			headerSize := int64(8)
			if boxSize == 1 {
				if _, err := reader.ReadAt(header[8:16], offset+8); err != nil {
					return err
				}
				boxSize = int64(binary.BigEndian.Uint64(header[8:16]))
				headerSize = 16
			} else if boxSize == 0 {
				boxSize = end - offset
			}
			if boxSize < headerSize || offset+boxSize > end {
				return errors.New("invalid mp4 atom")
			}
			payloadOffset, payloadSize := offset+headerSize, boxSize-headerSize
			switch kind {
			case "ftyp":
				foundFTYP = true
			case "moov", "trak":
				if err := walk(payloadOffset, payloadSize, depth+1); err != nil {
					return err
				}
			case "tkhd":
				if payloadSize >= 84 {
					tail := make([]byte, 44)
					if _, err := reader.ReadAt(tail, payloadOffset+payloadSize-44); err != nil {
						return err
					}
					w := int(binary.BigEndian.Uint32(tail[36:40]) >> 16)
					h := int(binary.BigEndian.Uint32(tail[40:44]) >> 16)
					a := int32(binary.BigEndian.Uint32(tail[0:4]))
					b := int32(binary.BigEndian.Uint32(tail[4:8]))
					c := int32(binary.BigEndian.Uint32(tail[12:16]))
					d := int32(binary.BigEndian.Uint32(tail[16:20]))
					if a == 0 && d == 0 && b != 0 && c != 0 {
						w, h = h, w
					}
					if w > 0 && h > 0 {
						width, height = w, h
					}
				}
			}
			offset += boxSize
		}
		return nil
	}
	if err := walk(0, size, 0); err != nil {
		return 0, 0, err
	}
	if !foundFTYP || width < 1 || height < 1 || width > 20000 || height > 20000 {
		return 0, 0, errors.New("missing mp4 metadata")
	}
	return width, height, nil
}

func validVideoFingerprint(value string) bool {
	if len(value) != sha1.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha1.Size && value == strings.ToLower(value)
}

func validVideoFilename(value string) bool {
	if !safeVideoFilename(value) {
		return false
	}
	return strings.EqualFold(filepath.Ext(value), ".mp4")
}

func safeVideoFilename(value string) bool {
	return value != "" && len([]rune(value)) <= 200 && !strings.ContainsAny(value, "/\\\x00\r\n") && filepath.Base(value) == value
}

func validVideoUploadID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func authorizeVideoUpload(scope tracking.Scope, session videoUploadSession) error {
	if !scope.Manage || scope.Tenant != session.Tenant || scope.Actor != session.Actor || !scope.Allows(session.OwnerID) {
		return &tracking.Error{Status: http.StatusForbidden, Message: "视频上传会话不可访问"}
	}
	return nil
}

func (session videoUploadSession) status() videoUploadStatus {
	parts := make([]int, 0, len(session.Parts))
	for raw := range session.Parts {
		part, err := strconv.Atoi(raw)
		if err == nil {
			parts = append(parts, part)
		}
	}
	sort.Ints(parts)
	return videoUploadStatus{UploadID: session.UploadID, ChunkSize: session.ChunkSize, UploadedParts: parts, Asset: session.Asset, Processing: session.Processing, Error: session.Error, Retryable: session.Retryable, ResetRequired: session.ResetNeeded}
}

func (session videoUploadSession) expectedPartSize(part int) (int64, bool) {
	if part < 0 || session.Size <= 0 || session.ChunkSize <= 0 {
		return 0, false
	}
	if int64(part) > int64(^uint64(0)>>1)/session.ChunkSize {
		return 0, false
	}
	start := int64(part) * session.ChunkSize
	if start < 0 || start >= session.Size {
		return 0, false
	}
	remaining := session.Size - start
	if remaining > session.ChunkSize {
		return session.ChunkSize, true
	}
	return remaining, true
}

func videoUploadPartPath(dir string, part int) string {
	return filepath.Join(dir, fmt.Sprintf("part-%010d", part))
}

func loadVideoUpload(root, uploadID string) (videoUploadSession, error) {
	var session videoUploadSession
	if !validVideoUploadID(uploadID) {
		return session, os.ErrNotExist
	}
	path := filepath.Join(root, uploadID, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return session, err
	}
	if err = json.Unmarshal(data, &session); err != nil || session.UploadID != uploadID || !validStoredMediaID(session.AssetID) || session.Tenant == "" || session.Actor <= 0 || session.Size <= 0 || !validVideoFingerprint(session.Fingerprint) || !validVideoFilename(session.Filename) || (session.Rights != "own" && session.Rights != "authorized") || session.ChunkSize != videoUploadChunkSize || session.Parts == nil {
		return videoUploadSession{}, contentstorage.ErrIntegrity
	}
	return session, nil
}

func saveVideoUpload(root string, session videoUploadSession) error {
	if !validVideoUploadID(session.UploadID) {
		return contentstorage.ErrInvalidInput
	}
	dir := filepath.Join(root, session.UploadID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".session-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(data)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "session.json"))
}

func cleanupExpiredVideoUploads(root string, now time.Time) {
	last := videoUploadLastCleanup.Load()
	if last != 0 && now.Unix()-last < int64(time.Hour/time.Second) {
		return
	}
	if !videoUploadLastCleanup.CompareAndSwap(last, now.Unix()) {
		return
	}
	cleanupExpiredVideoUploadsNow(root, now)
}

func cleanupExpiredVideoUploadsNow(root string, now time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("[video-upload] error operation=cleanup_scan error_type=%T", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validVideoUploadID(entry.Name()) {
			continue
		}
		key := root + ":" + entry.Name()
		if _, active := videoUploadWorkers.Load(key); active {
			continue
		}
		lock := videoUploadSessionLock(root, entry.Name())
		if !lock.TryLock() {
			continue
		}
		session, loadErr := loadVideoUpload(root, entry.Name())
		if loadErr != nil {
			log.Printf("[video-upload] error upload=%s operation=cleanup_load error_type=%T", entry.Name(), loadErr)
		}
		expired := loadErr == nil && ((session.Asset == nil && now.Sub(session.UpdatedAt) > videoUploadTTL) || (session.Asset != nil && now.Sub(session.UpdatedAt) > videoUploadDoneTTL))
		if expired {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				log.Printf("[video-upload] error upload=%s operation=cleanup_remove error_type=%T", entry.Name(), err)
			}
		}
		lock.Unlock()
	}
}

func videoUploadSessionLock(root, uploadID string) *sync.Mutex {
	digest := sha256.Sum256([]byte(root + ":" + uploadID))
	index := binary.BigEndian.Uint16(digest[:2]) % videoUploadLockCount
	return &videoUploadLocks[index]
}

func videoUploadLoadError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return &tracking.Error{Status: http.StatusNotFound, Message: "视频上传会话不存在"}
	}
	return err
}

func videoUploadPublicError(err error) string {
	var trackingErr *tracking.Error
	if errors.As(err, &trackingErr) && trackingErr.Status >= 400 && trackingErr.Status < 500 {
		return trackingErr.Message
	}
	return "视频处理失败，请重试"
}

func videoUploadFailure(err error) (retryable, resetRequired bool) {
	var trackingErr *tracking.Error
	if errors.As(err, &trackingErr) && trackingErr.Status >= 400 && trackingErr.Status < 500 {
		message := trackingErr.Message
		if strings.Contains(message, "指纹") || strings.Contains(message, "分片") || strings.Contains(message, "MP4") {
			return false, true
		}
		return false, false
	}
	return true, false
}
