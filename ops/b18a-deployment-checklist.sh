#!/bin/bash
# B18-A 链接导入配置验收——一键部署脚本
# 用法：用户提供三样材料后，在服务器上执行本脚本
# 前提：已在 servers 上部署最新后端（含 feat/phase3-supplement 分支代码）

set -euo pipefail

echo "=== B18-A 链接导入配置验收部署 ==="
echo ""

# 1. 抖一抖凭据配置
read -p "请输入抖一抖 AppId: " DOUYIDOU_APP_ID
read -sp "请输入抖一抖 AppSecret: " DOUYIDOU_APP_SECRET
echo ""

read -p "请输入元宝 Cookie 文件路径（绝对路径）: " YUANBAO_COOKIE_PATH

if [ ! -f "$YUANBAO_COOKIE_PATH" ]; then
  echo "❌ Cookie 文件不存在: $YUANBAO_COOKIE_PATH"
  exit 1
fi

COOKIE_PERMS=$(stat -c %a "$YUANBAO_COOKIE_PATH" 2>/dev/null || stat -f %Lp "$YUANBAO_COOKIE_PATH")
if [ "$COOKIE_PERMS" != "600" ]; then
  echo "⚠️  Cookie 文件权限为 $COOKIE_PERMS，修正为 600..."
  chmod 600 "$YUANBAO_COOKIE_PATH"
fi

COOKIE_SIZE=$(wc -c < "$YUANBAO_COOKIE_PATH")
if [ "$COOKIE_SIZE" -gt 16384 ]; then
  echo "❌ Cookie 文件超过 16KiB 限制（当前 ${COOKIE_SIZE} 字节）"
  exit 1
fi

# 2. 写入环境变量
ENV_FILE="${ENV_FILE:-/www/wwwroot/jiadilingguang/api/.env}"
if [ ! -f "$ENV_FILE" ]; then
  echo "⚠️  未找到 .env 文件（$ENV_FILE），将创建"
  touch "$ENV_FILE"
fi

cat >> "$ENV_FILE" << EOF
# B18-A 链接导入配置（$(date +%Y-%m-%d)）
CONTENT_IMPORT_DOUYIDOU_APP_ID=${DOUYIDOU_APP_ID}
CONTENT_IMPORT_DOUYIDOU_APP_SECRET=${DOUYIDOU_APP_SECRET}
CONTENT_IMPORT_YUANBAO_ENABLED=true
CONTENT_IMPORT_YUANBAO_COOKIE_FILE=${YUANBAO_COOKIE_PATH}
EOF

echo "✅ 环境变量已写入 $ENV_FILE"

# 3. 确认 COS 存储
if ! grep -q "CONTENT_MEDIA_STORAGE=cos" "$ENV_FILE"; then
  echo "CONTENT_MEDIA_STORAGE=cos" >> "$ENV_FILE"
  echo "✅ 已补 CONTENT_MEDIA_STORAGE=cos"
else
  echo "✅ CONTENT_MEDIA_STORAGE=cos 已配置"
fi

# 4. 执行数据库迁移
echo ""
echo "=== 数据库迁移 ==="
read -p "MySQL DSN (如 root:pass@tcp(127.0.0.1:3306)/jiadilinguang): " MYSQL_DSN

for SQL_FILE in phase3_writing_prompts_up.sql phase3_notification_prefs_up.sql phase3_intro_presets_up.sql; do
  SQL_PATH="$(dirname "$0")/../service/sql/$SQL_FILE"
  if [ -f "$SQL_PATH" ]; then
    echo "执行 $SQL_FILE ..."
    mysql --default-character-set=utf8mb4 -u "$(echo $MYSQL_DSN | sed 's/.*\/\/\([^:]*\):.*/\1/')" -p"$(echo $MYSQL_DSN | sed 's/.*:\([^@]*\)@.*/\1/')" "$(echo $MYSQL_DSN | sed 's/.*\/\([^?]*\).*/\1/')" < "$SQL_PATH" && echo "  ✅ $SQL_FILE" || echo "  ❌ $SQL_FILE 失败"
  fi
done

# 5. 重启后端
echo ""
echo "=== 重启后端服务 ==="
read -p "后端进程 PID 或 systemd 服务名: " SERVICE
if systemctl is-active --quiet "$SERVICE" 2>/dev/null; then
  systemctl restart "$SERVICE"
  echo "✅ systemd 服务 $SERVICE 已重启"
else
  echo "⚠️  非 systemd 服务，请手动重启后端进程"
fi

# 6. 验收提示
echo ""
echo "=== 部署完成，请验收 ==="
echo "1. 打开小程序 → 内容工作台 → 链接导入"
echo "2. 确认平台列表显示：微信公众号/抖音/小红书/快手/视频号"
echo "3. 逐平台粘贴真实样例链接提交导入"
echo "4. 确认导入后内容可正常阅读/播放/再次分享"
echo ""
echo "七类样例清单："
echo "  □ 抖音视频 ×3    □ 抖音图集 ×2"
echo "  □ 小红书视频 ×2   □ 小红书图文 ×2"
echo "  □ 快手视频 ×3    □ 快手图文 ×2"
echo "  □ 视频号 ×3（含 1 条疑似加密样本）"
