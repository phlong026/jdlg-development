package processor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"service/function"
	"service/model"
)

// PhoneAuthStrategy 手机号验证码登录策略
type PhoneAuthStrategy struct {
	userModel         *model.UserModel
	userProfileModel  *model.UserProfileModel
	codeSessionModel  *model.CodeSessionRedisModel
	userIdentityModel *model.UserIdentityModel
}

// NewPhoneAuthStrategy 创建手机号登录策略
func NewPhoneAuthStrategy(userModel *model.UserModel, userProfileModel *model.UserProfileModel, codeSessionModel *model.CodeSessionRedisModel, userIdentityModel *model.UserIdentityModel) *PhoneAuthStrategy {
	return &PhoneAuthStrategy{
		userModel:         userModel,
		userProfileModel:  userProfileModel,
		codeSessionModel:  codeSessionModel,
		userIdentityModel: userIdentityModel,
	}
}

// PhoneLoginRequest 手机号登录请求
type PhoneLoginRequest struct {
	Phone        string `json:"phone" binding:"required"`
	Code         string `json:"code" binding:"required"`
	DeviceID     string `json:"device_id"`
	IdentityType string `json:"identity_type"` // 乡墅业主/设计师/施工队/建材商/机构用户
	InviteCode   string `json:"invite_code"`
}

// GetStrategyName 获取策略名称
func (s *PhoneAuthStrategy) GetStrategyName() string {
	return "phone"
}

// phoneRegexp 手机号正则：11位纯数字，以1开头
var phoneRegexp = regexp.MustCompile(`^1\d{10}$`)

// Login 手机号验证码登录
func (s *PhoneAuthStrategy) Login(ctx context.Context, req interface{}) (*AuthResult, error) {
	loginReq, ok := req.(*PhoneLoginRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type")
	}

	phone := loginReq.Phone
	if !phoneRegexp.MatchString(phone) {
		return nil, errors.New("手机号格式不正确")
	}

	// 1. 通过 user_identities 表查找手机号绑定的用户
	userID, err := s.userIdentityModel.GetUserIDByPhone(phone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	isNewUser := false
	var user *model.User
	if errors.Is(err, sql.ErrNoRows) {
		// 新用户注册
		user, err = s.userModel.CreateByPhone(phone)
		if err != nil {
			return nil, fmt.Errorf("创建用户失败: %w", err)
		}

		// 创建 profile
		if s.userProfileModel != nil {
			if _, profileErr := s.userProfileModel.GetOrCreate(user.ID, loginReq.DeviceID); profileErr != nil {
				return nil, fmt.Errorf("创建用户资料失败: %w", profileErr)
			}
			// 设置身份类型
			if loginReq.IdentityType != "" {
				if identityErr := s.userProfileModel.UpdateIdentityType(user.ID, loginReq.IdentityType); identityErr != nil {
					return nil, fmt.Errorf("设置身份类型失败: %w", identityErr)
				}
			}
			// 更新手机号到 profile
			if phoneErr := s.userProfileModel.UpdatePhone(user.ID, phone); phoneErr != nil {
				return nil, fmt.Errorf("更新手机号失败: %w", phoneErr)
			}
		}

		// 绑定手机号到 user_identities
		if s.userIdentityModel != nil {
			if bindErr := s.userIdentityModel.BindPhone(user.ID, phone); bindErr != nil {
				return nil, fmt.Errorf("绑定手机号失败: %w", bindErr)
			}
		}

		isNewUser = true
	} else {
		// 已有用户，直接登录
		user, err = s.userModel.GetByID(userID)
		if err != nil {
			return nil, fmt.Errorf("获取用户失败: %w", err)
		}
	}

	// 解析合并后的有效用户
	if user != nil {
		user, err = s.userModel.GetEffectiveUserByID(user.ID)
		if err != nil {
			return nil, fmt.Errorf("解析有效用户失败: %w", err)
		}
	}

	// 创建 session
	// Code 需在 code_sessions.uk_code 唯一键下保证唯一，因此拼上手机号和 sessionID，
	// 避免不同号码在短时间内撞同一条 6 位短信验证码
	sessionID := function.GenerateSessionID(phone + loginReq.Code)
	codeSession := &model.CodeSession{
		Code:      "phone_" + phone + "_" + sessionID,
		DeviceID:  loginReq.DeviceID,
		SessionID: sessionID,
		UserID:    user.ID,
		IsBanned:  false,
	}
	if err := s.codeSessionModel.Create(codeSession); err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	return &AuthResult{
		User:        user,
		CodeSession: codeSession,
		IsNewUser:   isNewUser,
	}, nil
}
