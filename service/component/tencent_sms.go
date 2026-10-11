package component

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	sms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"

	"service/config"
)

const (
	smsCodePrefix  = "sms:code:"
	smsLimitPrefix = "sms:limit:"
	smsDailyPrefix = "sms:daily:"
	smsFailPrefix  = "sms:fail:"

	smsCooldownSeconds = 60
	smsDailyMax        = 10
	smsFailMaxAttempts = 5
)

// ErrSMSRateLimited 频率限制错误，路由层应返回 HTTP 429
type ErrSMSRateLimited struct {
	Message string
}

func (e *ErrSMSRateLimited) Error() string {
	return e.Message
}

// SendVerificationCode generates a 6-digit verification code, stores it in Redis,
// and sends it to the given phone number via Tencent Cloud SMS.
func SendVerificationCode(phone string, rdb *redis.Client) error {
	ctx := context.Background()
	cfg := config.Get().TencentSMS

	// Check cooldown: one message per 60 seconds
	limitKey := smsLimitPrefix + phone
	ttl, err := rdb.TTL(ctx, limitKey).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("检查发送间隔失败: %w", err)
	}
	if ttl > 0 {
		return &ErrSMSRateLimited{Message: fmt.Sprintf("发送过于频繁，请 %d 秒后重试", int(ttl.Seconds()))}
	}

	// Check daily limit
	dailyKey := smsDailyPrefix + phone
	count, err := rdb.Get(ctx, dailyKey).Int64()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("检查每日限额失败: %w", err)
	}
	if count >= smsDailyMax {
		return &ErrSMSRateLimited{Message: fmt.Sprintf("今日发送次数已达上限（%d 次）", smsDailyMax)}
	}

	// Generate 6-digit code (使用 crypto/rand 确保安全随机)
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return fmt.Errorf("生成验证码失败: %w", err)
	}
	code := fmt.Sprintf("%06d", n.Int64())

	// Determine expiry
	expireMinutes := cfg.ExpireMinutes
	if expireMinutes <= 0 {
		expireMinutes = 5
	}

	// Store code in Redis
	codeKey := smsCodePrefix + phone
	err = rdb.Set(ctx, codeKey, code, time.Duration(expireMinutes)*time.Minute).Err()
	if err != nil {
		return fmt.Errorf("存储验证码失败: %w", err)
	}

	// Set cooldown key
	err = rdb.Set(ctx, limitKey, "1", time.Duration(smsCooldownSeconds)*time.Second).Err()
	if err != nil {
		return fmt.Errorf("设置冷却键失败: %w", err)
	}

	// Increment daily counter (create with 24h TTL on first use)
	dailyCount, err := rdb.Incr(ctx, dailyKey).Result()
	if err != nil {
		return fmt.Errorf("递增每日计数失败: %w", err)
	}
	if dailyCount == 1 {
		now := time.Now()
		endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
		rdb.Expire(ctx, dailyKey, endOfDay.Sub(now)+time.Second)
	}

	// Reset fail counter on new code
	failKey := smsFailPrefix + phone
	rdb.Del(ctx, failKey)

	// Send SMS via Tencent Cloud SDK (skip if config is empty — development mode)
	if cfg.SecretID == "" || cfg.SecretKey == "" || cfg.SdkAppID == "" {
		fmt.Printf("[DEV] 验证码发送: phone=%s, code=%s\n", phone, code)
		return nil
	}

	// Send via Tencent Cloud SMS
	credential := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	cpf := profile.NewClientProfile()
	client, err := sms.NewClient(credential, "ap-guangzhou", cpf)
	if err != nil {
		return fmt.Errorf("创建短信客户端失败: %w", err)
	}

	request := sms.NewSendSmsRequest()
	request.SmsSdkAppId = common.StringPtr(cfg.SdkAppID)
	request.SignName = common.StringPtr(cfg.SignName)
	request.TemplateId = common.StringPtr(cfg.TemplateID)
	request.TemplateParamSet = common.StringPtrs([]string{
		code,
		strconv.Itoa(int(expireMinutes)),
	})
	request.PhoneNumberSet = common.StringPtrs([]string{"+86" + phone})

	response, err := client.SendSms(request)
	if err != nil {
		return fmt.Errorf("发送短信失败: %w", err)
	}

	if response != nil && response.Response != nil && len(response.Response.SendStatusSet) > 0 {
		status := response.Response.SendStatusSet[0]
		if status.Code != nil && *status.Code != "Ok" {
			errMsg := ""
			if status.Message != nil {
				errMsg = *status.Message
			}
			return fmt.Errorf("短信发送失败: %s", errMsg)
		}
	}

	return nil
}

// VerifyCode checks whether the provided verification code matches the one stored in Redis.
func VerifyCode(phone, code string, rdb *redis.Client) (bool, error) {
	ctx := context.Background()
	codeKey := smsCodePrefix + phone

	stored, err := rdb.Get(ctx, codeKey).Result()
	if err == redis.Nil {
		return false, nil // code expired or never sent
	}
	if err != nil {
		return false, fmt.Errorf("查询验证码失败: %w", err)
	}

	if stored != code {
		// Increment failure counter
		failKey := smsFailPrefix + phone
		failCount, _ := rdb.Incr(ctx, failKey).Result()
		if failCount >= smsFailMaxAttempts {
			rdb.Del(ctx, codeKey)
			rdb.Del(ctx, failKey)
			return false, fmt.Errorf("验证码已失效，请重新获取")
		}
		return false, nil
	}

	// Code is correct — clean up
	rdb.Del(ctx, codeKey)
	rdb.Del(ctx, smsFailPrefix+phone)

	return true, nil
}
