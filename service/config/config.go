package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// wechatpayConfigFile 仅用于从 certificate 目录下的 wechatpay_config.json 读取（环境变量未设置时）
type wechatpayConfigFile struct {
	MchID     string `json:"mch_id"`
	NotifyURL string `json:"notify_url"`
}

// virtualpayConfigFile 仅用于从 certificate 目录下的 virtualpay_config.json 读取（环境变量未设置时）
type virtualpayConfigFile struct {
	OfferID               string `json:"offer_id"`
	AppKeyProd            string `json:"app_key_prod"`
	AppKeySandbox         string `json:"app_key_sandbox"`
	MessageToken          string `json:"message_token"`
	MessageEncodingAESKey string `json:"message_encoding_aes_key"`
}

func initWechatPayConfig() WechatPayConfig {
	certDir := getEnv("WECHAT_PAY_CERT_DIR", "")
	if certDir == "" {
		if exec, err := os.Executable(); err == nil {
			certDir = filepath.Join(filepath.Dir(exec), "certificate")
		}
		if _, err := os.Stat(certDir); err != nil {
			certDir = "certificate"
		}
	}
	apiV3Key := getEnv("WECHAT_PAY_API_V3_KEY", "")
	if apiV3Key == "" && certDir != "" {
		if b, err := os.ReadFile(filepath.Join(certDir, "APIv3密钥.md")); err == nil {
			apiV3Key = strings.TrimSpace(string(b))
		}
	}
	mchID := strings.TrimSpace(getEnv("WECHAT_PAY_MCH_ID", ""))
	notifyURL := strings.TrimSpace(getEnv("WECHAT_PAY_NOTIFY_URL", ""))
	// 环境变量未设置时，从 certificate/wechatpay_config.json 读取（便于服务器未配置 env 时使用）
	if (mchID == "" || notifyURL == "") && certDir != "" {
		if b, err := os.ReadFile(filepath.Join(certDir, "wechatpay_config.json")); err == nil {
			var file wechatpayConfigFile
			if json.Unmarshal(b, &file) == nil {
				if mchID == "" && file.MchID != "" {
					mchID = strings.TrimSpace(file.MchID)
				}
				if notifyURL == "" && file.NotifyURL != "" {
					notifyURL = strings.TrimSpace(file.NotifyURL)
				}
			}
		}
	}
	enabledExplicit := getEnv("WECHAT_PAY_ENABLED", "")
	enabled := false
	if strings.ToLower(enabledExplicit) == "false" {
		enabled = false
	} else if enabledExplicit == "true" {
		enabled = true
	} else {
		enabled = mchID != "" && apiV3Key != "" && certDir != "" && notifyURL != ""
	}
	transferSceneId := strings.TrimSpace(getEnv("WECHAT_PAY_TRANSFER_SCENE_ID", "1000"))
	transferNotifyURL := strings.TrimSpace(getEnv("WECHAT_PAY_TRANSFER_NOTIFY_URL", ""))
	withdrawEnabled := getEnvBool("WECHAT_PAY_WITHDRAW_ENABLED", false)
	withdrawMinFen := getEnvInt64("WECHAT_PAY_WITHDRAW_MIN_FEN", 100)         // 默认 1 元
	withdrawStoneToFen := getEnvInt64("WECHAT_PAY_WITHDRAW_STONE_TO_FEN", 10) // 默认 1 灵石=10 分

	return WechatPayConfig{
		MchID:              mchID,
		APIv3Key:           apiV3Key,
		CertDir:            certDir,
		NotifyURL:          notifyURL,
		Enabled:            enabled,
		TransferSceneId:    transferSceneId,
		TransferNotifyURL:  transferNotifyURL,
		WithdrawEnabled:    withdrawEnabled,
		WithdrawMinFen:     withdrawMinFen,
		WithdrawStoneToFen: withdrawStoneToFen,
	}
}

// Config 应用配置
type Config struct {
	MySQL            MySQLConfig
	Redis            RedisConfig
	Wechat           WechatConfig
	WechatPay        WechatPayConfig
	VirtualPay       VirtualPayConfig
	EnterpriseWechat EnterpriseWechatConfig
	Server           ServerConfig
	COS              COSConfig
	AI               AIConfig
	AliyunSMS        AliyunSMSConfig
	TencentSMS       TencentSMSConfig
}

// AIConfig AI服务配置
// ArkAPIKey：火山引擎方舟（豆包聊天）使用，来自 ARK_API_KEY
// LaoZhangAPIKey：老张平台（生图 / AI 视频等）使用，来自 LAOZHANG_API_KEY
// ToAPIsAPIKey：ToAPIs（备用生图）使用，来自 TOAPIS_API_KEY
type AIConfig struct {
	ArkAPIKey                     string // 火山/豆包 Key
	LaoZhangAPIKey                string // 老张平台 Key
	ToAPIsAPIKey                  string // ToAPIs Key
	FastGPTBaseURL                string // FastGPT 服务地址
	FastGPTAPIKey                 string // FastGPT API Key
	FastGPTDatasetID              string // 建规快查公共知识库 ID
	FastGPTChatIDPrefix           string // 建规快查会话 ID 前缀
	VideoProvider                 string // AI 生成视频供应商：laozhang 或 ark_seedance，默认火山引擎方舟（豆包 Seedance）
	VideoAPIBaseURL               string // AI 生成视频接口 base URL，默认火山引擎方舟
	VideoModel                    string // AI 生成视频模型 ID，按供应商选择默认值，默认豆包 Seedance 1.5 Pro
	RunningHubAPIKey              string
	RunningHubTextToImageAppID    string
	RunningHubImageToImageAppID   string
	RunningHubInstanceType        string
	RunningHubUsePersonalQueue    string
	RunningHubPollIntervalSeconds int64
	RunningHubMaxWaitSeconds      int64
}

// MySQLConfig MySQL配置
type MySQLConfig struct {
	DSN string
}

// RedisConfig Redis配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// WechatConfig 微信小程序配置（用于登录、邀请海报小程序码等）
// 对应环境变量：WECHAT_APPID、WECHAT_APPSECRET
type WechatConfig struct {
	AppID     string // 小程序 AppID，必填
	AppSecret string // 小程序 AppSecret，必填
}

type EnterpriseWechatConfig struct {
	Download4KQRCodeURL   string
	Download4KTip         string
	ServicePhone          string
	CustomerServiceCorpID string
	CustomerServiceURL    string
	CallbackSecret        string
	CallbackMaxSkewSec    int64
}

// WechatPayConfig 微信支付 APIv3 配置（商户号、证书、APIv3 密钥）
// 证书目录默认 service/certificate，可配置 WECHAT_PAY_CERT_DIR
// APIv3 密钥可配置 WECHAT_PAY_API_V3_KEY，或从 certificate/APIv3密钥.md 读取
type WechatPayConfig struct {
	MchID     string // 商户号
	APIv3Key  string // APIv3 密钥（证书与回调解密）
	CertDir   string // 证书目录，内含 apiclient_key.pem、apiclient_cert.pem、pub_key.pem 等
	NotifyURL string // 支付结果回调地址，如 https://api.example.com/api/v1/wechatpay/notify
	Enabled   bool   // 是否启用微信支付

	// 商家转账（提现）相关
	TransferSceneId    string // 转账场景ID，如 1000-现金营销、1006-企业报销，需在商户平台申请
	TransferNotifyURL  string // 转账结果异步通知地址（可选）
	WithdrawEnabled    bool   // 是否开放提现
	WithdrawMinFen     int64  // 最低提现金额（分），如 100 表示 1 元
	WithdrawStoneToFen int64  // 灵石兑人民币比例：1 灵石 = WithdrawStoneToFen 分，如 10 表示 1 灵石=10 分
}

// VirtualPayConfig 微信小程序「虚拟支付（xpay）」配置。
//
// 与 WechatPayConfig 是两套独立通路：
//   - 充值/认证费/会员的下单与回调走 VirtualPay
//   - 提现仍走 WechatPay 商家转账（一期 D3 决策为提现暂停）
//
// 字段约束：
//   - OfferID / AppKeyProd / AppKeySandbox / MessageToken / MessageEncodingAESKey 来自 mp 后台「虚拟支付 → 基本配置」
//   - CoinRatio 必须与 mp 后台代币兑换比例完全一致（默认 10），发布后不可改
//   - SandboxMode 仅影响 Android/Windows/鸿蒙；iOS 通路在路由层强制 env=0
type VirtualPayConfig struct {
	Enabled               bool
	OfferID               string
	AppKeyProd            string
	AppKeySandbox         string
	MessageToken          string
	MessageEncodingAESKey string
	IOSEnabled            bool
	SandboxMode           bool
	CoinRatio             int64
	// 道具单价（分）。mp 后台「道具管理」发布后，必须用对应 env 写入实际金额。
	// 未配置时认证道具下单接口会返回 400，避免静默使用错误金额导致签名/对账问题。
	CertDesignerFen   int64
	CertEnterpriseFen int64
}

func initVirtualPayConfig() VirtualPayConfig {
	offerID := strings.TrimSpace(getEnv("VIRTUAL_PAY_OFFER_ID", ""))
	appKeyProd := strings.TrimSpace(getEnv("VIRTUAL_PAY_APP_KEY_PROD", ""))
	appKeySandbox := strings.TrimSpace(getEnv("VIRTUAL_PAY_APP_KEY_SANDBOX", ""))
	messageToken := strings.TrimSpace(getEnv("VIRTUAL_PAY_MESSAGE_TOKEN", ""))
	messageAESKey := strings.TrimSpace(getEnv("VIRTUAL_PAY_MESSAGE_AES_KEY", ""))

	// certDir 解析方式与 initWechatPayConfig 保持一致：env 优先，否则取可执行文件旁的 certificate 目录
	certDir := getEnv("WECHAT_PAY_CERT_DIR", "")
	if certDir == "" {
		if exec, err := os.Executable(); err == nil {
			certDir = filepath.Join(filepath.Dir(exec), "certificate")
		}
		if _, err := os.Stat(certDir); err != nil {
			certDir = "certificate"
		}
	}

	// 环境变量未设置时，从 certificate/virtualpay_config.json 读取
	if (offerID == "" || appKeyProd == "" || messageToken == "" || messageAESKey == "") && certDir != "" {
		if b, err := os.ReadFile(filepath.Join(certDir, "virtualpay_config.json")); err == nil {
			var file virtualpayConfigFile
			if json.Unmarshal(b, &file) == nil {
				if offerID == "" {
					offerID = strings.TrimSpace(file.OfferID)
				}
				if appKeyProd == "" {
					appKeyProd = strings.TrimSpace(file.AppKeyProd)
				}
				if appKeySandbox == "" {
					appKeySandbox = strings.TrimSpace(file.AppKeySandbox)
				}
				if messageToken == "" {
					messageToken = strings.TrimSpace(file.MessageToken)
				}
				if messageAESKey == "" {
					messageAESKey = strings.TrimSpace(file.MessageEncodingAESKey)
				}
			}
		}
	}

	enabledExplicit := getEnv("VIRTUAL_PAY_ENABLED", "")
	enabled := false
	if strings.ToLower(enabledExplicit) == "false" {
		enabled = false
	} else if enabledExplicit == "true" {
		enabled = true
	} else {
		// 未显式设置时：必填字段齐全才视为启用
		enabled = offerID != "" && appKeyProd != "" && messageToken != "" && messageAESKey != ""
	}

	return VirtualPayConfig{
		Enabled:               enabled,
		OfferID:               offerID,
		AppKeyProd:            appKeyProd,
		AppKeySandbox:         appKeySandbox,
		MessageToken:          messageToken,
		MessageEncodingAESKey: messageAESKey,
		IOSEnabled:            getEnvBool("VIRTUAL_PAY_IOS_ENABLED", true),
		SandboxMode:           getEnvBool("VIRTUAL_PAY_SANDBOX", false),
		CoinRatio:             getEnvInt64("VIRTUAL_PAY_COIN_RATIO", 10),
		CertDesignerFen:       getEnvInt64("VIRTUAL_PAY_CERT_DESIGNER_FEN", 0),
		CertEnterpriseFen:     getEnvInt64("VIRTUAL_PAY_CERT_ENTERPRISE_FEN", 0),
	}
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Addr          string
	SessionSecret string
	// SM2密钥（PEM格式）
	SM2PrivateKey string
	SM2PublicKey  string
	// SM4密钥（16字节，128位，Base64编码）
	SM4Key string
	// 环境标识：development 或 production
	Env string
}

// COSConfig 腾讯云对象存储配置
type COSConfig struct {
	SecretID            string // 腾讯云SecretID
	SecretKey           string // 腾讯云SecretKey
	Region              string // 地域，如：ap-beijing
	Bucket              string // 存储桶名称，格式：bucketname-appid
	Domain              string // 自定义域名（可选），如：https://cdn.example.com
	Prefix              string // 上传路径前缀，如 ai_assets/
	EnableSTS           bool   // 是否启用STS临时密钥
	STSRoleARN          string // STS角色ARN
	STSTokenDurationSec int64  // STS临时密钥有效期（秒）
}

type AliyunSMSConfig struct {
	AccessKeyID        string // 阿里云AccessKeyID
	AccessKeySecret    string // 阿里云AccessKeySecret
	PersonalAuthCode   string // 个人认证授权码
	EnterpriseAuthCode string // 企业认证授权码
}

// TencentSMSConfig 腾讯云短信配置
type TencentSMSConfig struct {
	SecretID      string // 腾讯云 SecretID
	SecretKey     string // 腾讯云 SecretKey
	SdkAppID      string // 短信应用 SdkAppID
	SignName      string // 短信签名
	TemplateID    string // 验证码模板ID
	ExpireMinutes int64  // 验证码有效期(分钟)，默认5
}

var globalConfig *Config

// Init 初始化配置
func Init() *Config {
	// 仅使用新的环境变量，不再兼容 AI_API_KEY
	arkKey := getEnv("ARK_API_KEY", "")
	laozhangKey := getEnv("LAOZHANG_API_KEY", "")
	toapisKey := getEnv("TOAPIS_API_KEY", "")
	globalConfig = &Config{
		MySQL: MySQLConfig{
			DSN: getEnv("MYSQL_DSN", "root:@tcp(127.0.0.1:3306)/jiadilinguang?charset=utf8mb4&parseTime=True&loc=Local"),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       0,
		},
		Wechat: WechatConfig{
			AppID:     getEnv("WECHAT_APPID", "wx94fdc48861913ede"),
			AppSecret: getEnv("WECHAT_APPSECRET", ""),
		},
		WechatPay:  initWechatPayConfig(),
		VirtualPay: initVirtualPayConfig(),
		EnterpriseWechat: EnterpriseWechatConfig{
			Download4KQRCodeURL:   strings.TrimSpace(getEnv("ENTERPRISE_WECHAT_DOWNLOAD_4K_QRCODE_URL", "")),
			Download4KTip:         strings.TrimSpace(getEnv("ENTERPRISE_WECHAT_DOWNLOAD_4K_TIP", "完成手机号授权验证后，可下载保存高清原图。")),
			ServicePhone:          strings.TrimSpace(getEnv("ENTERPRISE_WECHAT_SERVICE_PHONE", "13959877676")),
			CustomerServiceCorpID: strings.TrimSpace(getEnv("ENTERPRISE_WECHAT_CUSTOMER_SERVICE_CORP_ID", "")),
			CustomerServiceURL:    strings.TrimSpace(getEnv("ENTERPRISE_WECHAT_CUSTOMER_SERVICE_URL", "")),
			CallbackSecret:        strings.TrimSpace(getEnv("ENTERPRISE_WECHAT_CALLBACK_SECRET", "")),
			CallbackMaxSkewSec:    getEnvInt64("ENTERPRISE_WECHAT_CALLBACK_MAX_SKEW_SEC", 300),
		},
		Server: ServerConfig{
			Addr:          getEnv("HTTP_ADDR", ":8080"),
			SessionSecret: getEnv("SESSION_SECRET", "jiadilinguang-session-secret"),
			SM2PrivateKey: getEnv("SM2_PRIVATE_KEY", ""),
			SM2PublicKey:  getEnv("SM2_PUBLIC_KEY", ""),
			SM4Key:        getEnv("SM4_KEY", ""),
			Env:           getEnv("ENV", "development"),
		},
		COS: COSConfig{
			SecretID:            getEnv("COS_SECRET_ID", ""),
			SecretKey:           getEnv("COS_SECRET_KEY", ""),
			Region:              getEnv("COS_REGION", "ap-chongqing"),
			Bucket:              getEnv("COS_BUCKET", "jiadilingguangcos-1393500756"),
			Domain:              getEnv("COS_DOMAIN", "https://static.jiadilingguang.com"), // 自定义 CDN 域名，确保小程序合法域名可用
			Prefix:              getEnv("COS_PREFIX", "ai_assets/"),
			EnableSTS:           getEnvBool("COS_ENABLE_STS", false),
			STSRoleARN:          getEnv("COS_STS_ROLE_ARN", ""),
			STSTokenDurationSec: getEnvInt64("COS_STS_TOKEN_DURATION", 3600),
		},
		AI: AIConfig{
			ArkAPIKey:                     arkKey,
			LaoZhangAPIKey:                laozhangKey,
			ToAPIsAPIKey:                  toapisKey,
			FastGPTBaseURL:                strings.TrimRight(getEnv("FASTGPT_BASE_URL", ""), "/"),
			FastGPTAPIKey:                 getEnv("FASTGPT_API_KEY", ""),
			FastGPTDatasetID:              getEnv("FASTGPT_DATASET_ID", ""),
			FastGPTChatIDPrefix:           getEnv("FASTGPT_CHAT_ID_PREFIX", "building-code"),
			VideoProvider:                 getEnv("AI_VIDEO_PROVIDER", "ark_seedance"),
			VideoAPIBaseURL:               getEnv("AI_VIDEO_API_BASE_URL", "https://ark.cn-beijing.volces.com"),
			VideoModel:                    getEnv("AI_VIDEO_MODEL", "doubao-seedance-1-5-pro-251215"),
			RunningHubAPIKey:              getEnv("RUNNINGHUB_API_KEY", ""),
			RunningHubTextToImageAppID:    getEnv("RUNNINGHUB_TEXT_TO_IMAGE_APP_ID", "2049807115933499394"),
			RunningHubImageToImageAppID:   getEnv("RUNNINGHUB_IMAGE_TO_IMAGE_APP_ID", "2049809170538479617"),
			RunningHubInstanceType:        getEnv("RUNNINGHUB_INSTANCE_TYPE", "default"),
			RunningHubUsePersonalQueue:    getEnv("RUNNINGHUB_USE_PERSONAL_QUEUE", "false"),
			RunningHubPollIntervalSeconds: getEnvInt64("RUNNINGHUB_POLL_INTERVAL_SECONDS", 3),
			RunningHubMaxWaitSeconds:      getEnvInt64("RUNNINGHUB_MAX_WAIT_SECONDS", 180),
		},
		AliyunSMS: AliyunSMSConfig{
			AccessKeyID:        getEnv("ALIYUN_SMS_ACCESS_KEY_ID", ""),
			AccessKeySecret:    getEnv("ALIYUN_SMS_ACCESS_KEY_SECRET", ""),
			PersonalAuthCode:   getEnv("ALIYUN_SMS_PERSONAL_AUTH_CODE", ""),
			EnterpriseAuthCode: getEnv("ALIYUN_SMS_ENTERPRISE_AUTH_CODE", ""),
		},
		TencentSMS: TencentSMSConfig{
			SecretID:      getEnv("TENCENT_SMS_SECRET_ID", ""),
			SecretKey:     getEnv("TENCENT_SMS_SECRET_KEY", ""),
			SdkAppID:      getEnv("TENCENT_SMS_SDK_APP_ID", ""),
			SignName:      getEnv("TENCENT_SMS_SIGN_NAME", ""),
			TemplateID:    getEnv("TENCENT_SMS_TEMPLATE_ID", ""),
			ExpireMinutes: getEnvInt64("TENCENT_SMS_EXPIRE_MINUTES", 5),
		},
	}
	return globalConfig
}

// Get 获取全局配置
func Get() *Config {
	if globalConfig == nil {
		return Init()
	}
	return globalConfig
}

// getEnv 读取环境变量，带默认值
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getEnvBool 读取布尔环境变量，支持 "true"/"false"（忽略大小写）
func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		vLower := strings.ToLower(v)
		return vLower == "true" || vLower == "1" || vLower == "yes"
	}
	return def
}

// getEnvInt64 读取int64环境变量
func getEnvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return def
}

// IsDevelopment 判断是否为开发环境
func (c *Config) IsDevelopment() bool {
	return c.Server.Env == "development"
}

// IsProduction 判断是否为生产环境
func (c *Config) IsProduction() bool {
	return c.Server.Env == "production"
}
