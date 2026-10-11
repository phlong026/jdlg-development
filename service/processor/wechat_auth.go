package processor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"service/function"
	"service/model"
)

// WechatAuthStrategy 微信code登录策略
type WechatAuthStrategy struct {
	userModel        *model.UserModel
	codeSessionModel *model.CodeSessionRedisModel
	userProfileModel *model.UserProfileModel
	appID            string
	appSecret        string
}

// NewWechatAuthStrategy 创建微信登录策略
func NewWechatAuthStrategy(userModel *model.UserModel, codeSessionModel *model.CodeSessionRedisModel, appID, appSecret string) *WechatAuthStrategy {
	return &WechatAuthStrategy{
		userModel:        userModel,
		codeSessionModel: codeSessionModel,
		appID:            appID,
		appSecret:        appSecret,
	}
}

// NewWechatAuthStrategyWithProfile 创建微信登录策略（带用户profile验证）
func NewWechatAuthStrategyWithProfile(userModel *model.UserModel, codeSessionModel *model.CodeSessionRedisModel, userProfileModel *model.UserProfileModel, appID, appSecret string) *WechatAuthStrategy {
	return &WechatAuthStrategy{
		userModel:        userModel,
		codeSessionModel: codeSessionModel,
		userProfileModel: userProfileModel,
		appID:            appID,
		appSecret:        appSecret,
	}
}

// WechatLoginRequest 微信登录请求
type WechatLoginRequest struct {
	Code       string `json:"code" binding:"required"`
	DeviceID   string `json:"device_id"`   // 设备指纹（可选，优先使用）
	InviteCode string `json:"invite_code"` // 邀请码（可选，新用户注册时填写可得奖励）
}

// GetStrategyName 获取策略名称
func (s *WechatAuthStrategy) GetStrategyName() string {
	return "wechat"
}

// DeviceMismatchError 设备不匹配错误
type DeviceMismatchError struct {
	UserID   int64
	Username string
	Message  string
}

func (e *DeviceMismatchError) Error() string {
	return e.Message
}

func looksLikeLegacySessionOpenID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

// Login 微信code登录逻辑：
// 1. 先用微信 code 换取真实 openid / unionid
// 2. 按 openid / unionid 识别同一微信用户，保证跨设备仍落到同一账号
// 3. 如命中历史旧设备记录，则把真实微信身份回写到老账号，避免继续分叉出新账号
// 4. 每次登录创建独立 session_id + code_session，允许多设备并行登录
func (s *WechatAuthStrategy) Login(ctx context.Context, req interface{}) (*AuthResult, error) {
	loginReq, ok := req.(*WechatLoginRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type")
	}

	wechatSession, err := function.Code2Session(s.appID, s.appSecret, loginReq.Code)
	if err != nil {
		return nil, fmt.Errorf("wechat code2session failed: %w", err)
	}
	if wechatSession == nil || wechatSession.OpenID == "" {
		return nil, errors.New("微信登录态无效，未获取到 openid")
	}

	var user *model.User
	isNewUser := false
	user, err = s.userModel.GetByOpenID(wechatSession.OpenID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("get user by openid failed: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) && wechatSession.UnionID != "" {
		user, err = s.userModel.GetByUnionID(wechatSession.UnionID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get user by unionid failed: %w", err)
		}
		if err == nil {
			if bindErr := s.userModel.BindWechatIdentity(user.ID, wechatSession.OpenID, wechatSession.UnionID); bindErr != nil {
				return nil, fmt.Errorf("bind wechat identity by unionid failed: %w", bindErr)
			}
			user.OpenID = wechatSession.OpenID
			user.UnionID = wechatSession.UnionID
		}
	}

	if errors.Is(err, sql.ErrNoRows) && loginReq.DeviceID != "" {
		legacySession, legacyErr := s.codeSessionModel.GetByDeviceID(loginReq.DeviceID)
		if legacyErr == nil {
			legacyUser, userErr := s.userModel.GetByID(legacySession.UserID)
			if userErr != nil {
				if errors.Is(userErr, sql.ErrNoRows) {
					_ = s.codeSessionModel.Delete(legacySession)
				} else {
					return nil, fmt.Errorf("get legacy user failed: %w", userErr)
				}
			} else if legacyUser != nil && legacyUser.UserType == "miniprogram" && looksLikeLegacySessionOpenID(legacyUser.OpenID) {
				if bindErr := s.userModel.BindWechatIdentity(legacyUser.ID, wechatSession.OpenID, wechatSession.UnionID); bindErr != nil {
					return nil, fmt.Errorf("bind wechat identity by legacy session failed: %w", bindErr)
				}
				legacyUser.OpenID = wechatSession.OpenID
				legacyUser.UnionID = wechatSession.UnionID
				user = legacyUser
				err = nil
			}
		}
	}

	if errors.Is(err, sql.ErrNoRows) {
		user, err = s.userModel.CreateOrUpdateByOpenID(wechatSession.OpenID, wechatSession.UnionID)
		if err != nil {
			return nil, fmt.Errorf("create or get user failed: %w", err)
		}
		isNewUser = true
	} else if err != nil {
		return nil, fmt.Errorf("resolve user failed: %w", err)
	} else if user != nil && (user.OpenID != wechatSession.OpenID || (wechatSession.UnionID != "" && user.UnionID != wechatSession.UnionID)) {
		if bindErr := s.userModel.BindWechatIdentity(user.ID, wechatSession.OpenID, wechatSession.UnionID); bindErr != nil {
			return nil, fmt.Errorf("refresh wechat identity failed: %w", bindErr)
		}
		user.OpenID = wechatSession.OpenID
		user.UnionID = wechatSession.UnionID
	}

	if user != nil {
		user, err = s.userModel.GetEffectiveUserByID(user.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve effective user failed: %w", err)
		}
	}

	if s.userProfileModel != nil {
		s.userProfileModel.GetOrCreate(user.ID, "")
	}

	sessionID := function.GenerateSessionID(loginReq.Code)
	existingSession, err := s.codeSessionModel.GetBySessionID(sessionID)
	if err == nil && existingSession.IsBanned {
		return nil, errors.New("账号已被封禁，无法登录")
	}

	codeSession := &model.CodeSession{
		Code:      loginReq.Code,
		DeviceID:  loginReq.DeviceID,
		SessionID: sessionID,
		UserID:    user.ID,
		IsBanned:  false,
	}
	if err := s.codeSessionModel.Create(codeSession); err != nil {
		return nil, fmt.Errorf("create code session failed: %w", err)
	}

	return &AuthResult{
		User:        user,
		CodeSession: codeSession,
		IsNewUser:   isNewUser,
	}, nil
}
