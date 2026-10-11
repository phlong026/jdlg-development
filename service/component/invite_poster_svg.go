package component

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

// InvitePosterSVGPath 约定：邀请海报 SVG 模板文件名，放在 assets 目录下
const InvitePosterSVGPath = "invite_poster.svg"

// 占位符（与设计师约定，不解析 SVG 结构，仅做字符串替换）
const (
	PlaceholderQRCodeBase64 = "{qrcodebs64}" // 二维码图片 data URL（base64）
	PlaceholderNickname     = "{id}"         // 用户昵称
	PlaceholderInviteCode   = "{invi}"       // 邀请码
)

// loadInvitePosterSVGTemplate 从约定路径加载 SVG 模板，多路径尝试
func loadInvitePosterSVGTemplate() ([]byte, error) {
	tryPaths := []string{
		filepath.Join("component", "assets", InvitePosterSVGPath),
		filepath.Join("assets", InvitePosterSVGPath),
		InvitePosterSVGPath,
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		tryPaths = append(tryPaths,
			filepath.Join(exeDir, "assets", InvitePosterSVGPath),
			filepath.Join(exeDir, "component", "assets", InvitePosterSVGPath),
		)
	}
	for _, p := range tryPaths {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, os.ErrNotExist
}

// escapeXML 对昵称/邀请码做 XML 转义，避免破坏 SVG
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// qrcodeDataURL 将微信返回的图片转为 data URL（base64）
func qrcodeDataURL(imageData []byte) string {
	if len(imageData) == 0 {
		return ""
	}
	mime := "image/jpeg"
	if len(imageData) >= 8 && imageData[0] == 0x89 && imageData[1] == 'P' && imageData[2] == 'N' {
		mime = "image/png"
	}
	b64 := base64.StdEncoding.EncodeToString(imageData)
	return "data:" + mime + ";base64," + b64
}

// BuildInvitePosterFromSVG 使用 SVG 模板生成邀请海报：仅替换三个占位符，返回 SVG 内容（由前端用 canvas 转 PNG）
// 占位符：{qrcodebs64} 二维码 base64 data URL，{id} 昵称，{invi} 邀请码
func BuildInvitePosterFromSVG(inviteCode, nickname string, qrcodeImage []byte) (svgContent []byte, err error) {
	tpl, err := loadInvitePosterSVGTemplate()
	if err != nil {
		return nil, err
	}
	svg := string(tpl)
	dataURL := qrcodeDataURL(qrcodeImage)
	svg = strings.ReplaceAll(svg, PlaceholderQRCodeBase64, dataURL)
	svg = strings.ReplaceAll(svg, PlaceholderNickname, escapeXML(nickname))
	svg = strings.ReplaceAll(svg, PlaceholderInviteCode, escapeXML(inviteCode))
	return []byte(svg), nil
}
