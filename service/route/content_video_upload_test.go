package route

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func TestTrackingMP4DimensionsBoundsAtomCountAndHonorsCancellation(t *testing.T) {
	atom := []byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}
	tooMany := bytes.Repeat(atom, trackingMP4MaxAtoms+1)
	if _, _, err := trackingMP4DimensionsAtContext(context.Background(), bytes.NewReader(tooMany), int64(len(tooMany))); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("atom budget err=%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	valid := videoUploadMP4(1920, 1080)
	if _, _, err := trackingMP4DimensionsAtContext(ctx, bytes.NewReader(valid), int64(len(valid))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	width, height, err := trackingMP4DimensionsAt(bytes.NewReader(valid), int64(len(valid)))
	if err != nil || width != 1920 || height != 1080 {
		t.Fatalf("normal dimensions=%dx%d err=%v", width, height, err)
	}
}

func videoUploadMP4(width, height int) []byte {
	box := func(kind string, payload []byte) []byte {
		out := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(out[:4], uint32(len(out)))
		copy(out[4:8], kind)
		copy(out[8:], payload)
		return out
	}
	ftyp := box("ftyp", []byte("isom\x00\x00\x00\x00isommp42"))
	tkhdPayload := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhdPayload[len(tkhdPayload)-8:len(tkhdPayload)-4], uint32(width<<16))
	binary.BigEndian.PutUint32(tkhdPayload[len(tkhdPayload)-4:], uint32(height<<16))
	return append(ftyp, box("moov", box("trak", box("tkhd", tkhdPayload)))...)
}

func videoUploadPartRequest(t *testing.T, router http.Handler, uploadID string, part int, data []byte) *httptest.ResponseRecorder {
	return videoUploadPartReaderRequest(t, router, uploadID, part, bytes.NewReader(data))
}

func videoUploadPartReaderRequest(t *testing.T, router http.Handler, uploadID string, part int, source io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "part.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(file, source); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/workbench/media-uploads/"+uploadID+"/parts/"+strconv.Itoa(part), &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	return w
}

func TestVideoUploadOver50MiBCompletesAndStreamsRange(t *testing.T) {
	mediaRoot := t.TempDir()
	_ = os.Chmod(mediaRoot, 0700)
	t.Setenv("CONTENT_MEDIA_STORAGE", "filesystem")
	t.Setenv("TRACKING_MEDIA_DIR", mediaRoot)
	t.Setenv("CONTENT_VIDEO_UPLOAD_DIR", filepath.Join(mediaRoot, ".video-uploads"))
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, _ := newWorkbenchHandlerFixture(t, &scope)

	small := videoUploadMP4(1920, 1080)
	ftypSize := int(binary.BigEndian.Uint32(small[:4]))
	ftyp, moov := small[:ftypSize], small[ftypSize:]
	sourcePath := filepath.Join(t.TempDir(), "large.mp4")
	source, err := os.OpenFile(sourcePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Write(ftyp); err != nil {
		t.Fatal(err)
	}
	const mediaPayloadSize = int64(50 << 20)
	mdatHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(mdatHeader[:4], uint32(mediaPayloadSize+8))
	copy(mdatHeader[4:], "mdat")
	if _, err = source.Write(mdatHeader); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Seek(mediaPayloadSize-1, io.SeekCurrent); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Write(moov); err != nil {
		t.Fatal(err)
	}
	info, err := source.Stat()
	if err != nil || info.Size() <= 50<<20 {
		t.Fatalf("fixture size=%d err=%v", info.Size(), err)
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	fingerprintHash := sha1.New()
	if _, err = io.CopyBuffer(fingerprintHash, source, make([]byte, 32<<10)); err != nil {
		t.Fatal(err)
	}
	fingerprint := hex.EncodeToString(fingerprintHash.Sum(nil))
	begin := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads", map[string]any{
		"owner_id": "owner-a", "rights": "own", "filename": "large.mp4", "size": info.Size(), "fingerprint": fingerprint,
	})
	var started struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(begin.Body.Bytes(), &started); err != nil || started.Data.UploadID == "" {
		t.Fatal(begin.Code, begin.Body.String(), err)
	}
	for part, offset := 0, int64(0); offset < info.Size(); part, offset = part+1, offset+videoUploadChunkSize {
		length := videoUploadChunkSize
		if remaining := info.Size() - offset; remaining < length {
			length = remaining
		}
		response := videoUploadPartReaderRequest(t, router, started.Data.UploadID, part, io.NewSectionReader(source, offset, length))
		if response.Code != http.StatusOK {
			t.Fatalf("part %d=%d %s", part, response.Code, response.Body.String())
		}
	}
	if response := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads/"+started.Data.UploadID+"/complete", nil); response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var completed struct {
		Data struct {
			Asset *tracking.Asset `json:"asset"`
		} `json:"data"`
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status := workbenchRequest(t, router, http.MethodGet, "/workbench/media-uploads/"+started.Data.UploadID, nil)
		if err = json.Unmarshal(status.Body.Bytes(), &completed); err != nil {
			t.Fatal(err)
		}
		if completed.Data.Asset != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completed.Data.Asset == nil {
		t.Fatal("large upload did not complete")
	}
	wantTail := make([]byte, 8)
	if _, err = source.ReadAt(wantTail, info.Size()-8); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/workbench/media/"+completed.Data.Asset.ID, nil)
	request.Header.Set("Range", "bytes="+strconv.FormatInt(info.Size()-8, 10)+"-")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || !bytes.Equal(response.Body.Bytes(), wantTail) || response.Header().Get("Content-Range") == "" {
		t.Fatalf("range=%d body=%x want=%x headers=%v", response.Code, response.Body.Bytes(), wantTail, response.Header())
	}
}

func TestVideoUploadResumesAcrossHandlersAndCompletesIdempotently(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACKING_MEDIA_DIR", root)
	t.Setenv("CONTENT_VIDEO_UPLOAD_DIR", filepath.Join(root, ".video-uploads"))
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, svc := newWorkbenchHandlerFixture(t, &scope)
	payload := videoUploadMP4(1080, 1920)
	fingerprintBytes := sha1.Sum(payload)
	fingerprint := hex.EncodeToString(fingerprintBytes[:])
	begin := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads", map[string]any{
		"owner_id": "owner-a", "rights": "own", "filename": "villa.mp4", "size": len(payload), "fingerprint": fingerprint,
	})
	if begin.Code != http.StatusOK {
		t.Fatalf("begin=%d %s", begin.Code, begin.Body.String())
	}
	var started struct {
		Data struct {
			UploadID  string `json:"upload_id"`
			ChunkSize int64  `json:"chunk_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(begin.Body.Bytes(), &started); err != nil || started.Data.UploadID == "" || started.Data.ChunkSize != 4<<20 {
		t.Fatalf("started=%+v err=%v", started, err)
	}
	if upload := videoUploadPartRequest(t, router, started.Data.UploadID, 0, payload); upload.Code != http.StatusOK {
		t.Fatalf("part=%d %s", upload.Code, upload.Body.String())
	}

	// A new router/service instance must recover the same disk-backed session.
	restarted := gin.New()
	restartedGroup := restarted.Group("/workbench", func(c *gin.Context) { c.Set("workbench.scope", scope); c.Next() })
	registerContentWorkbenchHandlers(restartedGroup, svc, nil, nil)
	status := workbenchRequest(t, restarted, http.MethodGet, "/workbench/media-uploads/"+started.Data.UploadID, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"uploaded_parts":[0]`) {
		t.Fatalf("status=%d %s", status.Code, status.Body.String())
	}
	complete := workbenchRequest(t, restarted, http.MethodPost, "/workbench/media-uploads/"+started.Data.UploadID+"/complete", nil)
	if complete.Code != http.StatusOK || !strings.Contains(complete.Body.String(), `"processing":true`) {
		t.Fatalf("complete=%d %s", complete.Code, complete.Body.String())
	}
	var completed struct {
		Data struct {
			Asset *tracking.Asset `json:"asset"`
		} `json:"data"`
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		polled := workbenchRequest(t, restarted, http.MethodGet, "/workbench/media-uploads/"+started.Data.UploadID, nil)
		if err := json.Unmarshal(polled.Body.Bytes(), &completed); err != nil {
			t.Fatal(err)
		}
		if completed.Data.Asset != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if completed.Data.Asset == nil || completed.Data.Asset.Kind != "video" || completed.Data.Asset.Width != 1080 || completed.Data.Asset.Height != 1920 {
		t.Fatalf("completed=%+v", completed)
	}
	stored, err := os.ReadFile(filepath.Join(root, completed.Data.Asset.ID))
	if err != nil || !bytes.Equal(stored, payload) {
		t.Fatalf("stored bytes err=%v size=%d", err, len(stored))
	}
	if media, err := svc.Media(t.Context(), &scope, completed.Data.Asset.ID); err != nil || media.ID != completed.Data.Asset.ID {
		t.Fatalf("media=%+v err=%v", media, err)
	}
	retry := workbenchRequest(t, restarted, http.MethodPost, "/workbench/media-uploads/"+started.Data.UploadID+"/complete", nil)
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), completed.Data.Asset.ID) {
		t.Fatalf("retry=%d %s", retry.Code, retry.Body.String())
	}
	rebegin := workbenchRequest(t, restarted, http.MethodPost, "/workbench/media-uploads", map[string]any{
		"owner_id": "owner-a", "rights": "own", "filename": "renamed.mp4", "size": len(payload), "fingerprint": fingerprint,
	})
	if rebegin.Code != http.StatusOK || !strings.Contains(rebegin.Body.String(), started.Data.UploadID) || !strings.Contains(rebegin.Body.String(), completed.Data.Asset.ID) {
		t.Fatalf("rebegin=%d %s", rebegin.Code, rebegin.Body.String())
	}
	uploadRoot, _ := videoUploadRoot()
	session, err := loadVideoUpload(uploadRoot, started.Data.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	session.UpdatedAt = time.Now().Add(-videoUploadDoneTTL - time.Hour)
	if err = saveVideoUpload(uploadRoot, session); err != nil {
		t.Fatal(err)
	}
	cleanupExpiredVideoUploadsNow(uploadRoot, time.Now())
	if _, err = os.Stat(filepath.Join(uploadRoot, started.Data.UploadID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed session was not expired: %v", err)
	}
	recovered := workbenchRequest(t, restarted, http.MethodPost, "/workbench/media-uploads", map[string]any{
		"owner_id": "owner-a", "rights": "own", "filename": "villa.mp4", "size": len(payload), "fingerprint": fingerprint,
	})
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), completed.Data.Asset.ID) {
		t.Fatalf("expired session did not recover stored asset: %d %s", recovered.Code, recovered.Body.String())
	}
}

func TestVideoUploadRejectsWrongPartLengthAndRechecksScope(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	t.Setenv("TRACKING_MEDIA_DIR", root)
	t.Setenv("CONTENT_VIDEO_UPLOAD_DIR", filepath.Join(root, ".video-uploads"))
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, _ := newWorkbenchHandlerFixture(t, &scope)
	begin := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads", map[string]any{
		"owner_id": "owner-a", "rights": "authorized", "filename": "large.mp4", "size": (4 << 20) + 3, "fingerprint": strings.Repeat("b", 40),
	})
	var started struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(begin.Body.Bytes(), &started); err != nil || started.Data.UploadID == "" {
		t.Fatal(begin.Code, begin.Body.String(), err)
	}
	if wrong := videoUploadPartRequest(t, router, started.Data.UploadID, 0, []byte("short")); wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong=%d %s", wrong.Code, wrong.Body.String())
	}
	scope.Owners = []string{"owner-b"}
	if denied := workbenchRequest(t, router, http.MethodGet, "/workbench/media-uploads/"+started.Data.UploadID, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("denied=%d %s", denied.Code, denied.Body.String())
	}
}

func TestVideoUploadBeginHasNoBusinessSizeCap(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	t.Setenv("TRACKING_MEDIA_DIR", root)
	t.Setenv("CONTENT_VIDEO_UPLOAD_DIR", filepath.Join(root, ".video-uploads"))
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, _ := newWorkbenchHandlerFixture(t, &scope)
	w := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads", map[string]any{
		"owner_id": "owner-a", "rights": "own", "filename": "feature-film.mp4", "size": int64(8) << 30, "fingerprint": strings.Repeat("c", 40),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("begin large=%d %s", w.Code, w.Body.String())
	}
}

func TestVideoUploadCompletionRejectsMixedFileFingerprint(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	t.Setenv("TRACKING_MEDIA_DIR", root)
	t.Setenv("CONTENT_VIDEO_UPLOAD_DIR", filepath.Join(root, ".video-uploads"))
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, _ := newWorkbenchHandlerFixture(t, &scope)
	payload := videoUploadMP4(720, 1280)
	fingerprintBytes := sha1.Sum(payload)
	fingerprint := hex.EncodeToString(fingerprintBytes[:])
	corrupt := append([]byte(nil), payload...)
	corrupt[12] ^= 1
	begin := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads", map[string]any{
		"owner_id": "owner-a", "rights": "own", "filename": "mixed.mp4", "size": len(payload), "fingerprint": fingerprint,
	})
	var started struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(begin.Body.Bytes(), &started); err != nil || started.Data.UploadID == "" {
		t.Fatal(begin.Code, begin.Body.String(), err)
	}
	if part := videoUploadPartRequest(t, router, started.Data.UploadID, 0, corrupt); part.Code != http.StatusOK {
		t.Fatal(part.Code, part.Body.String())
	}
	if complete := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads/"+started.Data.UploadID+"/complete", nil); complete.Code != http.StatusOK {
		t.Fatal(complete.Code, complete.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := workbenchRequest(t, router, http.MethodGet, "/workbench/media-uploads/"+started.Data.UploadID, nil)
		if strings.Contains(status.Body.String(), `"processing":false`) && strings.Contains(status.Body.String(), `"reset_required":true`) {
			reset := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads", map[string]any{
				"owner_id": "owner-a", "rights": "own", "filename": "mixed.mp4", "size": len(payload), "fingerprint": fingerprint, "reset": true,
			})
			if reset.Code != http.StatusOK || !strings.Contains(reset.Body.String(), `"uploaded_parts":[]`) {
				t.Fatalf("reset=%d %s", reset.Code, reset.Body.String())
			}
			if part := videoUploadPartRequest(t, router, started.Data.UploadID, 0, payload); part.Code != http.StatusOK {
				t.Fatal(part.Code, part.Body.String())
			}
			if retry := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads/"+started.Data.UploadID+"/complete", nil); retry.Code != http.StatusOK {
				t.Fatal(retry.Code, retry.Body.String())
			}
			finishDeadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(finishDeadline) {
				finished := workbenchRequest(t, router, http.MethodGet, "/workbench/media-uploads/"+started.Data.UploadID, nil)
				if strings.Contains(finished.Body.String(), `"asset":`) {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("reset upload did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fingerprint mismatch did not fail")
}

func TestSlowPartDoesNotBlockAnotherUploadStatus(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	t.Setenv("TRACKING_MEDIA_DIR", root)
	t.Setenv("CONTENT_VIDEO_UPLOAD_DIR", filepath.Join(root, ".video-uploads"))
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, _ := newWorkbenchHandlerFixture(t, &scope)
	begin := func(fingerprint string, size int64) string {
		response := workbenchRequest(t, router, http.MethodPost, "/workbench/media-uploads", map[string]any{
			"owner_id": "owner-a", "rights": "own", "filename": "parallel.mp4", "size": size, "fingerprint": fingerprint,
		})
		var decoded struct {
			Data struct {
				UploadID string `json:"upload_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil || decoded.Data.UploadID == "" {
			t.Fatal(response.Code, response.Body.String(), err)
		}
		return decoded.Data.UploadID
	}
	slowID := begin(strings.Repeat("e", 40), videoUploadChunkSize)
	otherID := begin(strings.Repeat("f", 40), 1)
	for suffix := byte('g'); videoUploadSessionLock(root, slowID) == videoUploadSessionLock(root, otherID) && suffix <= 'z'; suffix++ {
		otherID = begin(strings.Repeat(string(suffix), 40), 1)
	}
	if videoUploadSessionLock(root, slowID) == videoUploadSessionLock(root, otherID) {
		t.Fatal("test could not select distinct lock stripes")
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		defer writer.Close()
		part, err := multipartWriter.CreateFormFile("file", "part.bin")
		if err != nil {
			return
		}
		if _, err = part.Write([]byte{0}); err != nil {
			return
		}
		close(started)
		<-release
		_, _ = part.Write(make([]byte, videoUploadChunkSize-1))
		_ = multipartWriter.Close()
	}()
	request := httptest.NewRequest(http.MethodPost, "/workbench/media-uploads/"+slowID+"/parts/0", reader)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	partDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		partDone <- response
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow upload did not start")
	}
	statusDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		statusDone <- workbenchRequest(t, router, http.MethodGet, "/workbench/media-uploads/"+otherID, nil)
	}()
	select {
	case status := <-statusDone:
		if status.Code != http.StatusOK {
			t.Fatalf("status=%d %s", status.Code, status.Body.String())
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("another upload status was blocked by slow network body")
	}
	close(release)
	if response := <-partDone; response.Code != http.StatusOK {
		t.Fatalf("part=%d %s", response.Code, response.Body.String())
	}
}

func TestVideoUploadCapabilityProbeDoesNotCreateDirectory(t *testing.T) {
	parent := t.TempDir()
	_ = os.Chmod(parent, 0700)
	root := filepath.Join(parent, "not-created")
	t.Setenv("CONTENT_VIDEO_UPLOAD_DIR", root)
	if !videoUploadConfigured() {
		t.Fatal("private parent should be a valid upload configuration")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capability probe created directory: %v", err)
	}
}

func TestVideoUploadFailureContractSeparatesRetryFromReset(t *testing.T) {
	if retry, reset := videoUploadFailure(errors.New("cos temporarily unavailable")); !retry || reset {
		t.Fatalf("transient failure retry=%v reset=%v", retry, reset)
	}
	if retry, reset := videoUploadFailure(&tracking.Error{Status: http.StatusConflict, Message: "视频指纹校验失败"}); retry || !reset {
		t.Fatalf("integrity failure retry=%v reset=%v", retry, reset)
	}
	if retry, reset := videoUploadFailure(&tracking.Error{Status: http.StatusConflict, Message: "媒体对象标识冲突"}); retry || reset {
		t.Fatalf("business conflict retry=%v reset=%v", retry, reset)
	}
}
