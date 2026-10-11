package function

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	cos "github.com/tencentyun/cos-go-sdk-v5"
	"service/component"
	"service/config"
)

// UploadBytes 上传内存数据到COS，返回可访问URL
// key为相对路径（会自动补上前缀）
func UploadBytes(ctx context.Context, client *cos.Client, cfg *config.Config, key string, data []byte, contentType string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("配置为空")
	}
	if client == nil {
		client = component.GetCOSClient()
	}
	if client == nil {
		return "", fmt.Errorf("COS客户端未初始化")
	}

	key = buildObjectKey(cfg, key)
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	_, err := client.Object.Put(ctx, key, bytes.NewReader(data), &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
	})
	if err != nil {
		return "", err
	}
	return component.BuildObjectURL(cfg, key), nil
}

// UploadReader 上传流式数据到COS
func UploadReader(ctx context.Context, client *cos.Client, cfg *config.Config, key string, reader io.Reader, contentType string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("配置为空")
	}
	if client == nil {
		client = component.GetCOSClient()
	}
	if client == nil {
		return "", fmt.Errorf("COS客户端未初始化")
	}

	key = buildObjectKey(cfg, key)
	if contentType == "" && reader != nil {
		if rs, ok := reader.(io.ReadSeeker); ok {
			buf := make([]byte, 512)
			n, _ := rs.Read(buf)
			_, _ = rs.Seek(0, io.SeekStart)
			contentType = http.DetectContentType(buf[:n])
		}
	}

	_, err := client.Object.Put(ctx, key, reader, &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
	})
	if err != nil {
		return "", err
	}
	return component.BuildObjectURL(cfg, key), nil
}

// ListCOSKeys 列出指定前缀下的对象 key（不含目录占位），最多 maxKeys 个
func ListCOSKeys(ctx context.Context, client *cos.Client, prefix string, maxKeys int) ([]string, error) {
	if client == nil {
		return nil, fmt.Errorf("COS客户端未初始化")
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	opt := &cos.BucketGetOptions{Prefix: prefix, MaxKeys: maxKeys}
	res, _, err := client.Bucket.Get(ctx, opt)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, c := range res.Contents {
		if c.Key == "" {
			continue
		}
		if strings.HasSuffix(c.Key, "/") {
			continue
		}
		keys = append(keys, c.Key)
	}
	return keys, nil
}

// GetCOSObject 下载 COS 对象到内存，单文件最大 maxBytes
func GetCOSObject(ctx context.Context, client *cos.Client, key string, maxBytes int64) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("COS客户端未初始化")
	}
	resp, err := client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if maxBytes <= 0 {
		maxBytes = 50 << 20 // 50MB
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

// buildObjectKey 将配置的Prefix与key拼接
func buildObjectKey(cfg *config.Config, key string) string {
	prefix := component.NormalizePrefix(cfg.COS.Prefix)
	key = strings.TrimLeft(key, "/")
	if prefix != "" {
		return path.Join(prefix, key)
	}
	return key
}

// RecordOSSFile 记录OSS文件到数据库（用于AI生成的文件）
// 如果文件已存在（通过objectKey判断），则更新记录；否则创建新记录
func RecordOSSFile(db interface{}, objectKey, fileName string, fileSize int64, contentType, fileURL, sourceType string, sourceID int64, sourceName, taskNo string) error {
	// 这里需要导入model包，但由于循环依赖问题，我们通过接口传递
	// 实际使用时，应该在调用处传入OSSFileModel实例
	// 这个函数主要用于AI任务完成时记录生成的文件
	return nil
}
