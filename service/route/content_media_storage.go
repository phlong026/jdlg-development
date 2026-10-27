package route

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"service/config"
	"service/contentimport"
	"service/contentstorage"
	"service/tracking"
)

func configureContentMedia(svc *tracking.Service, cfg *config.Config) {
	if os.Getenv("CONTENT_MEDIA_STORAGE") != "cos" {
		return
	}
	if cfg == nil {
		log.Printf("[content-media] error disabled reason=configuration_missing")
		return
	}
	bucket := os.Getenv("CONTENT_MEDIA_COS_BUCKET")
	if bucket == "" {
		bucket = cfg.COS.Bucket
	}
	region := os.Getenv("CONTENT_MEDIA_COS_REGION")
	if region == "" {
		region = cfg.COS.Region
	}
	store, err := contentstorage.NewCOSStore(&contentstorage.Config{Enabled: true, Bucket: bucket, Region: region, PrivatePrefix: os.Getenv("CONTENT_MEDIA_COS_PREFIX"), SecretID: cfg.COS.SecretID, SecretKey: cfg.COS.SecretKey})
	if err != nil {
		log.Printf("[content-media] error disabled reason=private_cos_configuration error_type=%T", err)
		return
	}
	svc.MediaObjects = store
}
func contentMediaConfigured(svc *tracking.Service) bool {
	switch os.Getenv("CONTENT_MEDIA_STORAGE") {
	case "cos":
		return svc != nil && svc.MediaObjects != nil
	case "", "filesystem":
		return workbenchMediaConfigured()
	default:
		return false
	}
}
func contentMediaLimit(asset tracking.Asset) int64 {
	if asset.Kind == "video" {
		return 50 << 20
	}
	return 10 << 20
}
func validStoredMediaID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func readContentMedia(ctx context.Context, svc *tracking.Service, id string, max int64) ([]byte, bool, error) {
	if !validStoredMediaID(id) {
		return nil, false, contentstorage.ErrInvalidInput
	}
	if svc.MediaObjects != nil {
		data, _, err := svc.MediaObjects.Get(ctx, svc.Config.Tenant, id, max)
		if errors.Is(err, contentstorage.ErrNotFound) {
			return nil, false, nil
		}
		return data, err == nil, err
	}
	root := os.Getenv("TRACKING_MEDIA_DIR")
	if !filepath.IsAbs(root) {
		return nil, false, contentstorage.ErrUnavailable
	}
	path := filepath.Join(root, id)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return nil, false, contentstorage.ErrIntegrity
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	closeErr := file.Close()
	if err != nil {
		return nil, false, err
	}
	if closeErr != nil {
		return nil, false, closeErr
	}
	if int64(len(data)) != info.Size() {
		return nil, false, contentstorage.ErrIntegrity
	}
	return data, true, nil
}
func storeContentMedia(ctx context.Context, svc *tracking.Service, id string, data []byte, mime string) error {
	if !validStoredMediaID(id) {
		return contentstorage.ErrInvalidInput
	}
	if svc.MediaObjects != nil {
		if err := svc.MediaObjects.Put(ctx, svc.Config.Tenant, id, data, mime); err != nil {
			log.Printf("[content-media] error asset=%s operation=private_put error_type=%T", id, err)
			if errors.Is(err, contentstorage.ErrPublicObject) {
				return &contentimport.ResolveError{Code: "private_storage_required"}
			}
			return err
		}
		return nil
	}
	root := os.Getenv("TRACKING_MEDIA_DIR")
	if !workbenchMediaConfigured() {
		return contentstorage.ErrUnavailable
	}
	temp, err := os.CreateTemp(root, ".import-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Link(name, filepath.Join(root, id)); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

// Call only after the caller verified the current content/share/owner grant.
func serveContentMediaBytes(c *gin.Context, svc *tracking.Service, asset tracking.Asset) {
	if !validStoredMediaID(asset.ID) {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", asset.MIME)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Security-Policy", "default-src 'none'")
	root := os.Getenv("TRACKING_MEDIA_DIR")
	if svc.MediaObjects == nil && filepath.IsAbs(root) {
		path := filepath.Join(root, asset.ID)
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > contentMediaLimit(asset) {
				c.Status(http.StatusNotFound)
				return
			}
			file, err := os.Open(path)
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			defer file.Close()
			http.ServeContent(c.Writer, c.Request, asset.ID, info.ModTime(), file)
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			c.Status(http.StatusServiceUnavailable)
			return
		}
	}
	if svc.MediaObjects == nil {
		c.Status(http.StatusNotFound)
		return
	}
	streamer, ok := svc.MediaObjects.(contentMediaStreamer)
	if !ok {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	trusted := trackingTrustedProxyPrefixes(os.Getenv("TRACKING_TRUSTED_PROXY_CIDRS"))
	if !contentMediaReadLimiter.allow(trackingRateKey(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"), trusted)) {
		c.Header("Retry-After", "1")
		c.Status(http.StatusTooManyRequests)
		return
	}
	select {
	case contentMediaReadSlots <- struct{}{}:
		defer func() { <-contentMediaReadSlots }()
	default:
		c.Header("Retry-After", "1")
		c.Status(http.StatusTooManyRequests)
		return
	}
	c.Header("Accept-Ranges", "bytes")
	if c.Request.Method == http.MethodHead {
		metadata, err := streamer.Head(c.Request.Context(), svc.Config.Tenant, asset.ID)
		if errors.Is(err, contentstorage.ErrNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		if err != nil || metadata.MIME != asset.MIME || metadata.Size <= 0 || metadata.Size > contentMediaLimit(asset) {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Header("Content-Length", strconv.FormatInt(metadata.Size, 10))
		c.Status(http.StatusOK)
		return
	}
	rangeHeader := c.GetHeader("Range")
	if c.GetHeader("If-Range") != "" {
		rangeHeader = ""
	}
	stream, err := streamer.Open(c.Request.Context(), svc.Config.Tenant, asset.ID, rangeHeader)
	if err != nil {
		var rangeError *contentstorage.RangeError
		if errors.As(err, &rangeError) {
			c.Header("Content-Range", "bytes */"+strconv.FormatInt(rangeError.Size, 10))
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		log.Printf("[content-media] error asset=%s operation=private_open error_type=%T", asset.ID, err)
		if errors.Is(err, contentstorage.ErrNotFound) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusServiceUnavailable)
		}
		return
	}
	defer stream.Body.Close()
	if stream.MIME != asset.MIME || stream.Size <= 0 || stream.Size > contentMediaLimit(asset) || stream.Length < 0 || stream.Length > stream.Size || (stream.Status != 200 && stream.Status != 206) {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("Content-Length", strconv.FormatInt(stream.Length, 10))
	if stream.ContentRange != "" {
		c.Header("Content-Range", stream.ContentRange)
	}
	controller := http.NewResponseController(c.Writer)
	if err = controller.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		log.Printf("[content-media] error asset=%s operation=write_deadline error_type=%T", asset.ID, err)
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(stream.Status)
	copied, copyErr := io.CopyBuffer(c.Writer, stream.Body, make([]byte, 32<<10))
	if copyErr != nil || copied != stream.Length {
		log.Printf("[content-media] error asset=%s operation=stream_copy error_type=%T bytes=%d", asset.ID, copyErr, copied)
		return
	}
	// Flush under the deadline, including buffered tail bytes. Only a completed
	// response may reset it; failed clients retain the bound while HTTP aborts.
	if err = controller.Flush(); err != nil {
		log.Printf("[content-media] error asset=%s operation=stream_flush error_type=%T", asset.ID, err)
		return
	}
	if err = controller.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("[content-media] error asset=%s operation=clear_deadline error_type=%T", asset.ID, err)
	}
}

// The route never falls back to whole-object Get for a range request.
type contentMediaStreamer interface {
	Head(context.Context, string, string) (contentstorage.Metadata, error)
	Open(context.Context, string, string, string) (*contentstorage.Stream, error)
}

var contentMediaReadSlots = make(chan struct{}, 8)
var contentMediaReadLimiter = newTrackingPeerLimiter(10000, 120, time.Now)
