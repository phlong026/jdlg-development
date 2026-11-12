package route

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"service/config"
	"service/function"
	"service/model"

	"github.com/tencentyun/cos-go-sdk-v5"
)

const (
	ossDeleteBatchLimit     = 100
	ossDeleteBatchBodyLimit = 16 << 10
	ossObjectDeleteTimeout  = 10 * time.Second
	ossBatchDeleteTimeout   = 30 * time.Second

	ossDeleteReasonStorageMissing = "storage_unavailable"
	ossDeleteReasonObjectFailed   = "object_delete_failed"
	ossDeleteReasonObjectKey      = "object_key_ambiguous"
	ossDeleteReasonRecordLoad     = "record_load_failed"
	ossDeleteReasonRecordFailed   = "record_delete_failed"
)

var (
	errOSSStorageUnavailable  = errors.New("OSS storage unavailable")
	errOSSObjectNotFound      = errors.New("OSS object not found")
	errOSSObjectKeyAmbiguous  = errors.New("OSS object key is ambiguous")
	errOSSRecordNotFound      = errors.New("OSS record not found")
	errOSSRecordLoadFailed    = errors.New("OSS record load failed")
	errOSSRecordDeleteFailed  = errors.New("OSS record delete failed")
	errOSSDeleteBatchTooLarge = errors.New("OSS delete batch too large")
	errOSSDeleteInvalidID     = errors.New("invalid OSS file id")
)

type ossFileDeleteStore interface {
	GetByID(id int64) (*model.OSSFile, error)
	Delete(id int64) error
}

type ossObjectDeleteFunc func(ctx context.Context, file *model.OSSFile) error

type ossDeleteFailure struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

type ossBatchDeleteResult struct {
	DeletedIDs        []int64            `json:"deleted_ids"`
	AlreadyMissingIDs []int64            `json:"already_missing_ids"`
	Failures          []ossDeleteFailure `json:"failures"`
}

func newCOSObjectDeleteFunc(client *cos.Client, cfg *config.Config) ossObjectDeleteFunc {
	if client == nil || cfg == nil {
		return nil
	}
	return func(ctx context.Context, file *model.OSSFile) error {
		objectKey, err := resolveManagedOSSObjectKey(cfg, file)
		if err != nil {
			return err
		}
		return deleteFullCOSObject(ctx, client, objectKey)
	}
}

func deleteFullCOSObject(ctx context.Context, client *cos.Client, objectKey string) error {
	resp, err := client.Object.Delete(ctx, objectKey)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return normalizeCOSObjectDeleteError(err)
}

// resolveManagedOSSObjectKey 不用当前 prefix 猜历史对象位置。
// 只接受当前可信 COS/CDN 主机上、且路径与库内 object_key 一致的 FileURL。
func resolveManagedOSSObjectKey(cfg *config.Config, file *model.OSSFile) (string, error) {
	if cfg == nil || file == nil {
		return "", errOSSObjectKeyAmbiguous
	}
	storedKey, ok := cleanOSSObjectKey(file.ObjectKey)
	if !ok {
		return "", errOSSObjectKeyAmbiguous
	}
	urlKey, err := trustedOSSURLObjectKey(cfg, file.FileURL)
	if err != nil {
		return "", errOSSObjectKeyAmbiguous
	}
	if urlKey == storedKey || strings.HasSuffix(urlKey, "/"+storedKey) {
		return urlKey, nil
	}
	return "", errOSSObjectKeyAmbiguous
}

func trustedOSSURLObjectKey(cfg *config.Config, rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errOSSObjectKeyAmbiguous
	}
	bases := make([]*url.URL, 0, 2)
	if domain := strings.TrimSpace(cfg.COS.Domain); domain != "" {
		if base, parseErr := url.Parse(domain); parseErr == nil {
			bases = append(bases, base)
		}
	}
	if cfg.COS.Bucket != "" && cfg.COS.Region != "" {
		base, _ := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.COS.Bucket, cfg.COS.Region))
		bases = append(bases, base)
	}
	for _, base := range bases {
		if base == nil || !strings.EqualFold(parsed.Scheme, base.Scheme) || !strings.EqualFold(parsed.Host, base.Host) {
			continue
		}
		basePath := strings.Trim(base.Path, "/")
		objectPath := strings.Trim(parsed.Path, "/")
		if basePath != "" {
			if objectPath == basePath || !strings.HasPrefix(objectPath, basePath+"/") {
				continue
			}
			objectPath = strings.TrimPrefix(objectPath, basePath+"/")
		}
		if key, valid := cleanOSSObjectKey(objectPath); valid {
			return key, nil
		}
	}
	return "", errOSSObjectKeyAmbiguous
}

func cleanOSSObjectKey(key string) (string, bool) {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" || key == "." || path.Clean(key) != key || strings.HasPrefix(key, "../") {
		return "", false
	}
	return key, true
}

func compensateUploadedOSSObject(ctx context.Context, client *cos.Client, cfg *config.Config, relativeKey string, timeout time.Duration) error {
	if client == nil || cfg == nil {
		return errOSSStorageUnavailable
	}
	fullKey, ok := cleanOSSObjectKey(function.ResolveCOSObjectKey(cfg, relativeKey))
	if !ok {
		return errOSSObjectKeyAmbiguous
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	err := deleteFullCOSObject(cleanupCtx, client, fullKey)
	if errors.Is(err, errOSSObjectNotFound) {
		return nil
	}
	return err
}

func normalizeCOSObjectDeleteError(err error) error {
	if err == nil {
		return nil
	}
	var responseErr *cos.ErrorResponse
	if errors.As(err, &responseErr) && responseErr.Response != nil && responseErr.Response.StatusCode == http.StatusNotFound {
		return errOSSObjectNotFound
	}
	return err
}

// deleteManagedOSSFile 先确认 COS 对象已删除（或已不存在），再删除数据库索引。
// 数据库删除失败时保留记录，下次重试会把 COS 404 视为幂等成功。
func deleteManagedOSSFile(ctx context.Context, store ossFileDeleteStore, deleteObject ossObjectDeleteFunc, id int64, timeout time.Duration) error {
	file, err := store.GetByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errOSSRecordNotFound
		}
		return fmt.Errorf("%w: %v", errOSSRecordLoadFailed, err)
	}
	if deleteObject == nil {
		return errOSSStorageUnavailable
	}

	deleteCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := deleteObject(deleteCtx, file); err != nil && !errors.Is(err, errOSSObjectNotFound) {
		return fmt.Errorf("delete OSS object: %w", err)
	}
	if err := store.Delete(id); err != nil {
		return fmt.Errorf("%w: %v", errOSSRecordDeleteFailed, err)
	}
	return nil
}

func deleteManagedOSSFiles(ctx context.Context, store ossFileDeleteStore, deleteObject ossObjectDeleteFunc, ids []int64, timeout time.Duration) ossBatchDeleteResult {
	result := ossBatchDeleteResult{
		DeletedIDs:        make([]int64, 0, len(ids)),
		AlreadyMissingIDs: make([]int64, 0),
		Failures:          make([]ossDeleteFailure, 0),
	}
	for _, id := range ids {
		err := deleteManagedOSSFile(ctx, store, deleteObject, id, timeout)
		if err == nil {
			result.DeletedIDs = append(result.DeletedIDs, id)
			continue
		}
		if errors.Is(err, errOSSRecordNotFound) {
			result.AlreadyMissingIDs = append(result.AlreadyMissingIDs, id)
			continue
		}
		result.Failures = append(result.Failures, ossDeleteFailure{ID: id, Reason: classifyOSSDeleteFailure(err)})
	}
	return result
}

func classifyOSSDeleteFailure(err error) string {
	switch {
	case errors.Is(err, errOSSStorageUnavailable):
		return ossDeleteReasonStorageMissing
	case errors.Is(err, errOSSRecordLoadFailed):
		return ossDeleteReasonRecordLoad
	case errors.Is(err, errOSSObjectKeyAmbiguous):
		return ossDeleteReasonObjectKey
	case errors.Is(err, errOSSRecordDeleteFailed):
		return ossDeleteReasonRecordFailed
	default:
		return ossDeleteReasonObjectFailed
	}
}

func normalizeOSSDeleteIDs(ids []int64, limit int) ([]int64, error) {
	seen := make(map[int64]struct{}, len(ids))
	normalized := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errOSSDeleteInvalidID
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
		if len(normalized) > limit {
			return nil, errOSSDeleteBatchTooLarge
		}
	}
	return normalized, nil
}
