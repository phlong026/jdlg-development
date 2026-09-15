package function

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"golang.org/x/crypto/bcrypt"
	"strings"
)

// HashPassword 密码加密（使用MD5，生产环境建议使用bcrypt）
func HashPassword(password string) string {
	hash := md5.Sum([]byte(password + "jiadilinguang_salt"))
	return hex.EncodeToString(hash[:])
}

// VerifyPassword 验证密码
func VerifyPassword(password, hashedPassword string) bool {
	if strings.HasPrefix(hashedPassword, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(HashPassword(password)), []byte(hashedPassword)) == 1
}

// HashManagementPassword uses a salted adaptive hash for new CRM accounts.
func HashManagementPassword(password string) (string, error) {
	data, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(data), err
}
