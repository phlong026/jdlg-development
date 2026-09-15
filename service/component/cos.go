package component

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	cos "github.com/tencentyun/cos-go-sdk-v5"
	sts "github.com/tencentyun/qcloud-cos-sts-sdk/go"
	"service/config"
)

var defaultCOSClient *cos.Client

// COSCredential 用于初始化COS客户端的凭证
type COSCredential struct {
	SecretID     string
	SecretKey    string
	SessionToken string
}

// InitCOSClient 初始化COS客户端并缓存默认实例
func InitCOSClient(cfg *config.Config, cred *COSCredential) (*cos.Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("配置为空")
	}
	if cfg.COS.Bucket == "" || cfg.COS.Region == "" {
		return nil, fmt.Errorf("COS Bucket 或 Region 未配置")
	}
	if cred == nil {
		cred = &COSCredential{
			SecretID:     cfg.COS.SecretID,
			SecretKey:    cfg.COS.SecretKey,
			SessionToken: "",
		}
	}
	if cred.SecretID == "" || cred.SecretKey == "" {
		return nil, fmt.Errorf("COS凭证未配置")
	}

	baseURL, err := buildBucketURL(cfg.COS.Bucket, cfg.COS.Region)
	if err != nil {
		return nil, err
	}
	client := cos.NewClient(baseURL, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:     cred.SecretID,
			SecretKey:    cred.SecretKey,
			SessionToken: cred.SessionToken,
		},
	})
	defaultCOSClient = client
	return client, nil
}

// GetCOSClient 返回已初始化的默认COS客户端
func GetCOSClient() *cos.Client {
	return defaultCOSClient
}

// BuildObjectURL 根据配置构建访问URL（若配置了自定义域名则使用）
func BuildObjectURL(cfg *config.Config, key string) string {
	if cfg.COS.Domain != "" {
		return strings.TrimRight(cfg.COS.Domain, "/") + "/" + strings.TrimLeft(key, "/")
	}
	return fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", cfg.COS.Bucket, cfg.COS.Region, strings.TrimLeft(key, "/"))
}

// NormalizePrefix 确保前缀以 / 结尾
func NormalizePrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	if !strings.HasSuffix(prefix, "/") {
		return prefix + "/"
	}
	return prefix
}

// RequestSTSCredential 使用长期密钥向STS申请临时密钥
func RequestSTSCredential(cfg *config.Config) (*sts.CredentialResult, error) {
	if cfg.COS.SecretID == "" || cfg.COS.SecretKey == "" {
		return nil, fmt.Errorf("COS SecretId/SecretKey 未配置")
	}
	if cfg.COS.STSRoleARN == "" {
		return nil, fmt.Errorf("COS_STS_ROLE_ARN 未配置")
	}
	appID := extractAppID(cfg.COS.Bucket)
	if appID == "" {
		return nil, fmt.Errorf("无法从Bucket解析出APPID，请确认命名格式为 name-appid")
	}

	// 创建STS客户端
	client := sts.NewClient(
		cfg.COS.SecretID,
		cfg.COS.SecretKey,
		nil,
	)

	prefix := NormalizePrefix(cfg.COS.Prefix)
	resource := fmt.Sprintf("qcs::cos:%s:uid/%s:prefix//%s/%s/%s*", cfg.COS.Region, appID, appID, cfg.COS.Bucket, prefix)

	policy := &sts.CredentialPolicy{
		Version: "2.0",
		Statement: []sts.CredentialPolicyStatement{
			{
				Action: []string{
					"name/cos:PutObject",
					"name/cos:PostObject",
					"name/cos:InitiateMultipartUpload",
					"name/cos:ListMultipartUploads",
					"name/cos:ListParts",
					"name/cos:UploadPart",
					"name/cos:CompleteMultipartUpload",
				},
				Effect:   "allow",
				Resource: []string{resource},
			},
		},
	}

	opt := &sts.CredentialOptions{
		Region:          cfg.COS.Region,
		DurationSeconds: cfg.COS.STSTokenDurationSec,
		RoleArn:         cfg.COS.STSRoleARN,
		Policy:          policy,
	}
	return client.GetCredential(opt)
}

func buildBucketURL(bucket, region string) (*cos.BaseURL, error) {
	u, err := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", bucket, region))
	if err != nil {
		return nil, err
	}
	return &cos.BaseURL{BucketURL: u}, nil
}

func extractAppID(bucket string) string {
	if bucket == "" {
		return ""
	}
	parts := strings.Split(bucket, "-")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}

// HealthCheck 校验COS配置与可用性（只做配置检查，不发起网络请求）
func HealthCheck(cfg *config.Config) error {
	if cfg.COS.Bucket == "" || cfg.COS.Region == "" {
		return fmt.Errorf("COS Bucket 或 Region 未配置")
	}
	// 如果启用了STS但未配置RoleARN，给出警告但不阻止启动
	if cfg.COS.EnableSTS && cfg.COS.STSRoleARN == "" {
		return fmt.Errorf("已启用STS但COS_STS_ROLE_ARN未配置，STS功能将不可用")
	}
	return nil
}

// PingBucket 发起简单的HEAD请求验证连通性（可选）
func PingBucket(ctx context.Context, client *cos.Client) error {
	if client == nil {
		return fmt.Errorf("COS客户端未初始化")
	}
	_, err := client.Bucket.Head(ctx)
	return err
}
