package main

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"service/component"
	"service/config"

	"github.com/redis/go-redis/v9"
)

// ============================================================
// 重复账号检测与合并工具
//
// 使用方式:
//   go run ./cmd/merge_duplicate_users --env bin/.env --mode detect
//   go run ./cmd/merge_duplicate_users --env bin/.env --mode merge --source 101 --target 100
//   go run ./cmd/merge_duplicate_users --env bin/.env --mode auto-detect-merge
//
// 模式说明:
//   detect            - 仅检测重复账号，输出列表供人工审核
//   merge             - 将 source 用户合并到 target 用户（需指定 --source 和 --target）
//   auto-detect-merge - 自动检测并逐对确认合并（交互式）
// ============================================================

type duplicatePair struct {
	WechatUserID int64  // 微信注册的账号 ID
	PhoneUserID  int64  // 手机号注册的账号 ID
	Phone        string // 共同的手机号
	// 额外信息（用于展示）
	WechatUsername string
	PhoneUsername  string
	WechatCreated  string
	PhoneCreated   string
}

func main() {
	envPath := flag.String("env", filepath.Join("bin", ".env"), "env file path")
	mode := flag.String("mode", "detect", "运行模式: detect, merge, auto-detect-merge")
	sourceID := flag.Int64("source", 0, "被合并用户ID（merge模式必填）")
	targetID := flag.Int64("target", 0, "保留用户ID（merge模式必填）")
	dryRun := flag.Bool("dry-run", false, "仅模拟执行，不实际修改数据")
	flag.Parse()

	if err := loadEnvFile(*envPath); err != nil && !os.IsNotExist(err) {
		fail(err)
	}

	cfg := config.Init()
	db, err := component.InitMySQL(cfg)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	rdb, err := component.InitRedis(cfg)
	if err != nil {
		fail(err)
	}

	switch *mode {
	case "detect":
		pairs := detectDuplicates(db)
		printDuplicateReport(pairs, db)
	case "merge":
		if *sourceID <= 0 || *targetID <= 0 {
			fail(fmt.Errorf("merge模式需要指定 --source 和 --target"))
		}
		if *sourceID == *targetID {
			fail(fmt.Errorf("source 和 target 不能是同一个用户"))
		}
		executeMerge(db, rdb, *sourceID, *targetID, *dryRun)
	case "auto-detect-merge":
		pairs := detectDuplicates(db)
		if len(pairs) == 0 {
			log.Println("✅ 未检测到重复账号")
			return
		}
		printDuplicateReport(pairs, db)
		fmt.Printf("\n共检测到 %d 对重复账号，是否逐对确认合并？(y/n): ", len(pairs))
		if !confirmInput() {
			log.Println("已取消")
			return
		}
		for i, pair := range pairs {
			fmt.Printf("\n[%d/%d] 准备合并: 手机用户 %d → 微信用户 %d (手机号: %s)\n",
				i+1, len(pairs), pair.PhoneUserID, pair.WechatUserID, pair.Phone)
			fmt.Print("  确认合并？(y=合并 / n=跳过 / q=退出): ")
			switch readChoice() {
			case "y":
				executeMerge(db, rdb, pair.PhoneUserID, pair.WechatUserID, *dryRun)
			case "q":
				log.Println("用户终止")
				return
			default:
				log.Printf("  跳过 %d → %d", pair.PhoneUserID, pair.WechatUserID)
			}
		}
	default:
		fail(fmt.Errorf("未知模式: %s", *mode))
	}
}

// ============================================================
// 检测逻辑
// ============================================================

func detectDuplicates(db *sql.DB) []duplicatePair {
	var pairs []duplicatePair

	// 策略1: user_profiles.phone 相同，但属于不同用户
	//   找出：一个用户有 openid（微信账号），另一个 username 以 phone_ 开头（手机账号）
	query := `
SELECT 
    wechat_user.id AS wechat_user_id,
    wechat_user.username AS wechat_username,
    wechat_user.created_at AS wechat_created,
    phone_user.id AS phone_user_id,
    phone_user.username AS phone_username,
    phone_user.created_at AS phone_created,
    wp.phone
FROM user_profiles wp
INNER JOIN users wechat_user 
    ON wechat_user.id = wp.user_id 
    AND wechat_user.user_type = 'miniprogram' 
    AND wechat_user.merge_status = 'normal'
    AND wechat_user.openid IS NOT NULL 
    AND wechat_user.openid != ''
INNER JOIN user_identities ui 
    ON ui.identity_type = 'phone' 
    AND ui.identity_key = wp.phone
INNER JOIN users phone_user 
    ON phone_user.id = ui.user_id 
    AND phone_user.user_type = 'miniprogram' 
    AND phone_user.merge_status = 'normal'
    AND phone_user.id != wechat_user.id
WHERE wp.phone IS NOT NULL AND wp.phone != ''
ORDER BY wechat_user.id
`
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("⚠️ 策略1查询失败: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var p duplicatePair
			if err := rows.Scan(&p.WechatUserID, &p.WechatUsername, &p.WechatCreated,
				&p.PhoneUserID, &p.PhoneUsername, &p.PhoneCreated, &p.Phone); err != nil {
				log.Printf("⚠️ 扫描行失败: %v", err)
				continue
			}
			pairs = append(pairs, p)
		}
	}

	// 策略2: user_identities 中同一手机号绑了多个用户（理论上唯一约束不允许，但防万一）
	query2 := `
SELECT ui.identity_key AS phone, 
       GROUP_CONCAT(ui.user_id ORDER BY ui.user_id) AS user_ids,
       COUNT(DISTINCT ui.user_id) AS cnt
FROM user_identities ui
INNER JOIN users u ON u.id = ui.user_id AND u.user_type = 'miniprogram' AND u.merge_status = 'normal'
WHERE ui.identity_type = 'phone'
GROUP BY ui.identity_key
HAVING cnt > 1
`
	rows2, err2 := db.Query(query2)
	if err2 != nil {
		log.Printf("⚠️ 策略2查询失败: %v", err2)
	} else {
		defer rows2.Close()
		for rows2.Next() {
			var phone, userIDsStr string
			var cnt int
			if err := rows2.Scan(&phone, &userIDsStr, &cnt); err != nil {
				continue
			}
			log.Printf("⚠️ 异常: 手机号 %s 绑定了 %d 个用户: %s", phone, cnt, userIDsStr)
		}
	}

	// 策略3: 同一 device_id 下有多个用户（辅助信号，仅报告，不自动合并）
	query3 := `
SELECT p.device_id, COUNT(DISTINCT p.user_id) AS cnt,
       GROUP_CONCAT(DISTINCT p.user_id ORDER BY p.user_id) AS user_ids
FROM user_profiles p
INNER JOIN users u ON u.id = p.user_id AND u.user_type = 'miniprogram' AND u.merge_status = 'normal'
WHERE p.device_id IS NOT NULL AND p.device_id != '' AND p.device_id NOT LIKE 'temp_%'
GROUP BY p.device_id
HAVING cnt > 1
ORDER BY cnt DESC
LIMIT 20
`
	rows3, err3 := db.Query(query3)
	if err3 != nil {
		log.Printf("⚠️ 策略3查询失败: %v", err3)
	} else {
		defer rows3.Close()
		fmt.Println("\n--- 同设备多账号（辅助参考，不自动合并）---")
		for rows3.Next() {
			var deviceID, userIDs string
			var cnt int
			if err := rows3.Scan(&deviceID, &cnt, &userIDs); err != nil {
				continue
			}
			fmt.Printf("  设备 %s: %d 个账号 [%s]\n", deviceID[:min(20, len(deviceID))], cnt, userIDs)
		}
	}

	return pairs
}

func printDuplicateReport(pairs []duplicatePair, db *sql.DB) {
	if len(pairs) == 0 {
		fmt.Println("\n✅ 未检测到可自动匹配的重复账号")
		return
	}

	fmt.Printf("\n=== 检测到 %d 对重复账号（同一手机号关联到不同用户）===\n\n", len(pairs))
	fmt.Printf("%-5s | %-10s %-20s %-20s | %-10s %-20s %-20s | %-12s\n",
		"#", "微信用户ID", "微信用户名", "微信注册时间",
		"手机用户ID", "手机用户名", "手机注册时间", "共同手机号")
	fmt.Println(strings.Repeat("-", 130))

	for i, p := range pairs {
		fmt.Printf("%-5d | %-10d %-20s %-20s | %-10d %-20s %-20s | %-12s\n",
			i+1, p.WechatUserID, truncate(p.WechatUsername, 18), p.WechatCreated,
			p.PhoneUserID, truncate(p.PhoneUsername, 18), p.PhoneCreated, p.Phone)
	}

	fmt.Println("\n合并建议：将手机账号(PhoneUserID)合并到微信账号(WechatUserID)")
	fmt.Println("原因：微信账号通常是用户的主要账号，保留 openid 可确保微信登录正常")
}

// ============================================================
// 合并执行逻辑
// ============================================================

func executeMerge(db *sql.DB, rdb *redis.Client, sourceID, targetID int64, dryRun bool) {
	log.Printf("🔄 开始合并: 用户 %d → 用户 %d (dry-run: %v)", sourceID, targetID, dryRun)

	// 验证两个用户都存在且状态正常
	var sourceStatus, targetStatus string
	err := db.QueryRow("SELECT merge_status FROM users WHERE id = ? AND user_type = 'miniprogram'", sourceID).Scan(&sourceStatus)
	if err != nil {
		log.Printf("❌ 源用户 %d 不存在或查询失败: %v", sourceID, err)
		return
	}
	err = db.QueryRow("SELECT merge_status FROM users WHERE id = ? AND user_type = 'miniprogram'", targetID).Scan(&targetStatus)
	if err != nil {
		log.Printf("❌ 目标用户 %d 不存在或查询失败: %v", targetID, err)
		return
	}
	if sourceStatus != "normal" {
		log.Printf("❌ 源用户 %d 已被合并(status=%s)，跳过", sourceID, sourceStatus)
		return
	}
	if targetStatus != "normal" {
		log.Printf("❌ 目标用户 %d 状态异常(status=%s)，跳过", targetID, targetStatus)
		return
	}

	if dryRun {
		log.Printf("  [DRY-RUN] 以下操作将被执行:")
		printMergePlan(db, rdb, sourceID, targetID)
		return
	}

	// 开始事务
	tx, err := db.Begin()
	if err != nil {
		log.Printf("❌ 开启事务失败: %v", err)
		return
	}
	defer tx.Rollback()

	var totalAffected int64

	// 1. 迁移灵石记录
	affected := execMigrate(tx, "stone_records",
		"UPDATE stone_records SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 2. 迁移模板解锁（跳过已存在的）
	affected = execMigrate(tx, "template_unlocks",
		`INSERT IGNORE INTO template_unlocks (user_id, template_id, created_at)
		 SELECT ?, template_id, created_at FROM template_unlocks WHERE user_id = ?`, targetID, sourceID)
	execMigrate(tx, "template_unlocks(清理源)",
		"DELETE FROM template_unlocks WHERE user_id = ?", sourceID)
	totalAffected += affected

	// 3. 迁移收藏（跳过已存在的）
	affected = execMigrate(tx, "user_favorites",
		`UPDATE IGNORE user_favorites SET user_id = ? WHERE user_id = ?`, targetID, sourceID)
	execMigrate(tx, "user_favorites(清理冲突)",
		"DELETE FROM user_favorites WHERE user_id = ?", sourceID)
	totalAffected += affected

	// 4. 迁移点赞（跳过已存在的）
	affected = execMigrate(tx, "template_likes",
		`UPDATE IGNORE template_likes SET user_id = ? WHERE user_id = ?`, targetID, sourceID)
	execMigrate(tx, "template_likes(清理冲突)",
		"DELETE FROM template_likes WHERE user_id = ?", sourceID)
	totalAffected += affected

	// 5. 迁移评论
	affected = execMigrate(tx, "template_comments",
		"UPDATE template_comments SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 6. 迁移分享记录
	affected = execMigrate(tx, "template_shares",
		"UPDATE template_shares SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 7. 迁移订单
	affected = execMigrate(tx, "user_orders",
		"UPDATE user_orders SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 8. 迁移会员资格
	affected = execMigrate(tx, "user_memberships",
		"UPDATE user_memberships SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 9. 迁移AI任务
	affected = execMigrate(tx, "ai_tasks",
		"UPDATE ai_tasks SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 10. 迁移AI视频任务
	affected = execMigrate(tx, "ai_video_tasks",
		"UPDATE ai_video_tasks SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 11. 迁移签到记录
	affected = execMigrate(tx, "checkin_records",
		"UPDATE checkin_records SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 12. 迁移邀请关系（邀请人身份）
	affected = execMigrate(tx, "invite_relations(inviter)",
		"UPDATE IGNORE invite_relations SET inviter_user_id = ? WHERE inviter_user_id = ?", targetID, sourceID)
	totalAffected += affected
	// 迁移邀请关系（被邀请人身份）
	affected = execMigrate(tx, "invite_relations(invitee)",
		"UPDATE IGNORE invite_relations SET invitee_user_id = ? WHERE invitee_user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 13. 迁移邀请奖励
	affected = execMigrate(tx, "invite_rewards",
		"UPDATE invite_rewards SET inviter_id = ? WHERE inviter_id = ?", targetID, sourceID)
	totalAffected += affected

	// 14. 迁移工单
	affected = execMigrate(tx, "support_tickets",
		"UPDATE support_tickets SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 15. 迁移认证申请
	affected = execMigrate(tx, "certification_applications",
		"UPDATE certification_applications SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 16. 迁移关注关系（跳过冲突）
	affected = execMigrate(tx, "designer_follows",
		"UPDATE IGNORE designer_follows SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	execMigrate(tx, "designer_follows(清理冲突)",
		"DELETE FROM designer_follows WHERE user_id = ?", sourceID)
	totalAffected += affected

	// 17. 迁移设计师评价
	affected = execMigrate(tx, "designer_reviews",
		"UPDATE designer_reviews SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 18. 迁移灵感资源归属
	affected = execMigrate(tx, "inspiration_assets",
		"UPDATE inspiration_assets SET creator_user_id = ? WHERE creator_user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 19. 迁移模板归属
	affected = execMigrate(tx, "templates",
		"UPDATE templates SET creator_user_id = ? WHERE creator_user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 20. 迁移 code_sessions（保证旧 token 仍指向有效用户）
	affected = execMigrate(tx, "code_sessions",
		"UPDATE code_sessions SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	totalAffected += affected

	// 21. 迁移 user_identities（把源用户的登录凭证转给目标用户）
	affected = execMigrate(tx, "user_identities",
		"UPDATE IGNORE user_identities SET user_id = ? WHERE user_id = ?", targetID, sourceID)
	execMigrate(tx, "user_identities(清理冲突)",
		"DELETE FROM user_identities WHERE user_id = ?", sourceID)
	totalAffected += affected

	// 22. 合并 user_profiles（保留目标用户的 profile，补充缺失字段）
	mergeProfiles(tx, sourceID, targetID)

	// 23. 标记源用户为已合并
	execMigrate(tx, "users(标记合并)",
		"UPDATE users SET merge_status = 'merged_source', merged_to_user_id = ?, updated_at = NOW() WHERE id = ?",
		targetID, sourceID)

	// 提交事务
	if err := tx.Commit(); err != nil {
		log.Printf("❌ 事务提交失败: %v", err)
		return
	}

	// 24. 合并 Redis 灵石余额（事务外执行）
	mergeRedisStones(rdb, sourceID, targetID)

	log.Printf("✅ 合并完成: 用户 %d → 用户 %d, 共迁移 %d 条记录", sourceID, targetID, totalAffected)
}

func execMigrate(tx *sql.Tx, tableName string, query string, args ...interface{}) int64 {
	result, err := tx.Exec(query, args...)
	if err != nil {
		log.Printf("  ⚠️ %s 迁移失败: %v", tableName, err)
		return 0
	}
	affected, _ := result.RowsAffected()
	if affected > 0 {
		log.Printf("  ✓ %s: 迁移 %d 条", tableName, affected)
	}
	return affected
}

func mergeProfiles(tx *sql.Tx, sourceID, targetID int64) {
	// 将源用户 profile 中目标用户缺失的字段补充过去
	// 重点：手机号、身份类型、昵称
	_, err := tx.Exec(`
		UPDATE user_profiles target_p
		INNER JOIN user_profiles source_p ON source_p.user_id = ?
		SET 
			target_p.phone = CASE 
				WHEN (target_p.phone IS NULL OR target_p.phone = '') AND source_p.phone != '' 
				THEN source_p.phone ELSE target_p.phone END,
			target_p.identity_type = CASE 
				WHEN (target_p.identity_type IS NULL OR target_p.identity_type = '') AND source_p.identity_type != '' 
				THEN source_p.identity_type ELSE target_p.identity_type END,
			target_p.nickname = CASE 
				WHEN (target_p.nickname IS NULL OR target_p.nickname = '') AND source_p.nickname != '' 
				THEN source_p.nickname ELSE target_p.nickname END,
			target_p.updated_at = NOW()
		WHERE target_p.user_id = ?
	`, sourceID, targetID)
	if err != nil {
		log.Printf("  ⚠️ user_profiles 合并失败: %v", err)
	} else {
		log.Printf("  ✓ user_profiles: 已合并缺失字段")
	}
}

func mergeRedisStones(rdb *redis.Client, sourceID, targetID int64) {
	if rdb == nil {
		return
	}
	ctx := context.Background()
	sourceKey := "user:stones:" + strconv.FormatInt(sourceID, 10)
	targetKey := "user:stones:" + strconv.FormatInt(targetID, 10)

	sourceStones, err := rdb.Get(ctx, sourceKey).Int64()
	if err != nil || sourceStones <= 0 {
		return
	}

	// 将源用户灵石加到目标用户
	rdb.IncrBy(ctx, targetKey, sourceStones)
	// 清零源用户灵石
	rdb.Set(ctx, sourceKey, 0, 0)

	log.Printf("  ✓ Redis灵石: 转移 %d 灵石到目标用户", sourceStones)
}

func printMergePlan(db *sql.DB, rdb *redis.Client, sourceID, targetID int64) {
	tables := []struct {
		name  string
		query string
	}{
		{"stone_records", "SELECT COUNT(*) FROM stone_records WHERE user_id = ?"},
		{"template_unlocks", "SELECT COUNT(*) FROM template_unlocks WHERE user_id = ?"},
		{"user_favorites", "SELECT COUNT(*) FROM user_favorites WHERE user_id = ?"},
		{"template_likes", "SELECT COUNT(*) FROM template_likes WHERE user_id = ?"},
		{"template_comments", "SELECT COUNT(*) FROM template_comments WHERE user_id = ?"},
		{"user_orders", "SELECT COUNT(*) FROM user_orders WHERE user_id = ?"},
		{"user_memberships", "SELECT COUNT(*) FROM user_memberships WHERE user_id = ?"},
		{"ai_tasks", "SELECT COUNT(*) FROM ai_tasks WHERE user_id = ?"},
		{"checkin_records", "SELECT COUNT(*) FROM checkin_records WHERE user_id = ?"},
		{"code_sessions", "SELECT COUNT(*) FROM code_sessions WHERE user_id = ?"},
		{"user_identities", "SELECT COUNT(*) FROM user_identities WHERE user_id = ?"},
	}

	for _, t := range tables {
		var cnt int
		if err := db.QueryRow(t.query, sourceID).Scan(&cnt); err == nil && cnt > 0 {
			log.Printf("  📦 %s: %d 条待迁移", t.name, cnt)
		}
	}

	if rdb != nil {
		ctx := context.Background()
		sourceKey := "user:stones:" + strconv.FormatInt(sourceID, 10)
		stones, err := rdb.Get(ctx, sourceKey).Int64()
		if err == nil && stones > 0 {
			log.Printf("  💎 Redis灵石: %d 待转移", stones)
		}
	}
}

// ============================================================
// 工具函数
// ============================================================

func confirmInput() bool {
	reader := bufio.NewReader(os.Stdin)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(text)) == "y"
}

func readChoice() string {
	reader := bufio.NewReader(os.Stdin)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(text))
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"'")
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func fail(err error) {
	log.Fatalf("❌ 致命错误: %v", err)
}
