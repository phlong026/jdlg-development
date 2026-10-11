package processor

import (
	"context"
	"errors"
	"fmt"
	"service/function"
	"service/model"
	"strings"
)

// PasswordAuthStrategy 账号密码登录策略
type PasswordAuthStrategy struct {
	userModel        *model.UserModel
	userType         string // 用户类型：'management' 或 'miniprogram'
	userProfileModel *model.UserProfileModel
}

// NewPasswordAuthStrategy 创建账号密码登录策略
func NewPasswordAuthStrategy(userModel *model.UserModel, userType string, userProfileModel *model.UserProfileModel) *PasswordAuthStrategy {
	if userType == "" {
		userType = "management" // 默认管理后台
	}
	return &PasswordAuthStrategy{
		userModel:        userModel,
		userType:         userType,
		userProfileModel: userProfileModel,
	}
}

// PasswordLoginRequest 账号密码登录请求
type PasswordLoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	DeviceID string `json:"device_id"`
}

// GetStrategyName 获取策略名称
func (s *PasswordAuthStrategy) GetStrategyName() string {
	return "password"
}

// Login 账号密码登录
func (s *PasswordAuthStrategy) Login(ctx context.Context, req interface{}) (*AuthResult, error) {
	loginReq, ok := req.(*PasswordLoginRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type")
	}

	// 1. 根据用户名和用户类型查询用户
	user, err := s.userModel.GetByUsernameAndType(loginReq.Username, s.userType)
	if err != nil {
		return nil, errors.New("用户名或密码错误")
	}

	// 2. 验证密码
	if !function.VerifyPassword(loginReq.Password, user.Password) {
		return nil, errors.New("用户名或密码错误")
	}

	// Upgrade the representation after a valid legacy login, without changing the
	// user's password. Conditional update cannot overwrite a concurrent reset.
	if s.userType == "management" && !strings.HasPrefix(user.Password, "$2") {
		if encoded, hashErr := function.HashManagementPassword(loginReq.Password); hashErr == nil {
			_, _ = s.userModel.DB.ExecContext(ctx, "UPDATE users SET password=? WHERE id=? AND password=?", encoded, user.ID, user.Password)
		}
	}
	effectiveUser, err := s.userModel.GetEffectiveUserByID(user.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve effective user failed: %w", err)
	}

	return &AuthResult{
		User: effectiveUser,
	}, nil
}
