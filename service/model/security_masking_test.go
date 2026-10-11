package model

import (
	"errors"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "空密钥返回空", input: "", want: ""},
		{name: "短密钥只保留首字符", input: "sk-123", want: "s****"},
		{name: "恰好 8 位只保留首字符", input: "12345678", want: "1****"},
		{name: "常规密钥保留首尾各 4 位", input: "sk-abcdef1234567890wxyz", want: "sk-a****wxyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskAPIKey(tt.input); got != tt.want {
				t.Fatalf("MaskAPIKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMaskAPIKeyDoesNotLeakMiddle(t *testing.T) {
	key := "sk-secret-middle-part-9876"
	masked := MaskAPIKey(key)
	if masked == key {
		t.Fatalf("掩码不能等于原密钥")
	}
	if len(masked) >= len(key) {
		t.Fatalf("掩码长度 %d 应明显短于原密钥 %d", len(masked), len(key))
	}
}

func TestIsIdempotentAlterSkip(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "1060 列已存在跳过", err: &mysql.MySQLError{Number: 1060, Message: "Duplicate column name 'session_no'"}, want: true},
		{name: "1061 索引已存在跳过", err: &mysql.MySQLError{Number: 1061, Message: "Duplicate key name 'idx_session_no'"}, want: true},
		{name: "语法错误不跳过", err: &mysql.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax"}, want: false},
		{name: "普通错误不跳过", err: errors.New("connection refused"), want: false},
		{name: "nil 不跳过", err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIdempotentAlterSkip(tt.err); got != tt.want {
				t.Fatalf("isIdempotentAlterSkip(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
