package processor

import (
	"context"
	"service/model"
)

// AuthStrategy 登录策略接口
type AuthStrategy interface {
	Login(ctx context.Context, req interface{}) (*AuthResult, error)
	GetStrategyName() string
}

// AuthResult 登录结果
type AuthResult struct {
	User        *model.User
	CodeSession *model.CodeSession
	IsNewUser   bool // 是否为新注册用户
}

// AuthProcessor 认证处理器
type AuthProcessor struct {
	strategies map[string]AuthStrategy
}

// NewAuthProcessor 创建认证处理器
func NewAuthProcessor() *AuthProcessor {
	return &AuthProcessor{
		strategies: make(map[string]AuthStrategy),
	}
}

// RegisterStrategy 注册登录策略
func (p *AuthProcessor) RegisterStrategy(strategy AuthStrategy) {
	p.strategies[strategy.GetStrategyName()] = strategy
}

// GetStrategy 获取登录策略
func (p *AuthProcessor) GetStrategy(name string) (AuthStrategy, bool) {
	strategy, ok := p.strategies[name]
	return strategy, ok
}
