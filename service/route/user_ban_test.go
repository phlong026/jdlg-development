package route

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"service/model"
)

// 封禁状态机 + 管理列表筛选/统计的 MySQL 集成测试。
// 仅在使用显式提供的空一次性测试库时运行（与 wecom_identity_test 同一模式）：
//
//	CREATE DATABASE jd_user_ban_test; USER_BAN_TEST_DSN='user:pass@tcp(127.0.0.1:13306)/jd_user_ban_test' go test ./route -run TestUserBan
func TestUserBanFlowAndListFilters(t *testing.T) {
	dsn := os.Getenv("USER_BAN_TEST_DSN")
	if dsn == "" {
		t.Skip("set USER_BAN_TEST_DSN to an isolated jd_user_ban_test database")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "jd_user_ban_test") {
		t.Fatal("refusing non-test database")
	}
	db, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("test database must be empty: %d %v", count, err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}

	userDB := model.NewUserModel(db)
	if err := userDB.InitTable(); err != nil {
		t.Fatal(err)
	}
	// stones 列由 UserRedisModel 初始化（仅操作 DB，redis client 可为 nil）
	if err := model.NewUserRedisModel(db, nil).InitStonesColumn(); err != nil {
		t.Fatal(err)
	}
	profileDB := model.NewUserProfileModel(db)
	if err := profileDB.InitTable(); err != nil {
		t.Fatal(err)
	}
	membershipDB := model.NewUserMembershipModel(db)
	if err := membershipDB.InitTable(); err != nil {
		t.Fatal(err)
	}
	orderDB := model.NewUserOrderModel(db)
	if err := orderDB.InitTable(); err != nil {
		t.Fatal(err)
	}

	// 两个小程序用户 + 一个管理端用户（管理端不应出现在列表/统计里）
	exec(`INSERT INTO users (id, username, user_type, stones, created_at, updated_at) VALUES
		(101, 'wx_normal', 'miniprogram', 500, NOW(), NOW()),
		(102, 'wx_bad',    'miniprogram',   0, NOW(), NOW()),
		(201, 'admin_x',   'management',  999, NOW(), NOW())`)
	exec(`INSERT INTO user_profiles (user_id, nickname, phone, identity_type, enterprise_wechat_verified, enterprise_wechat_contact, has_password) VALUES
		(101, '正常用户', '13861026102', '乡墅业主', 1, '13861026102', 1),
		(102, '风险用户', '',            '',          0, '',             0)`)
	exec(`INSERT INTO user_orders (user_id, order_no, type, order_category, amount, status, created_at) VALUES
		(102, 'R001', 'recharge', 'recharge', 100, 'success', NOW())`)

	t.Run("ban and unban state machine", func(t *testing.T) {
		if err := userDB.SetUserBanned(102, true, "同设备批量注册刷签到"); err != nil {
			t.Fatal(err)
		}
		banned, reason, err := userDB.GetUserBanStatus(102)
		if err != nil || !banned {
			t.Fatalf("expected banned=true, got %v %v", banned, err)
		}
		if reason != "同设备批量注册刷签到" {
			t.Fatalf("reason = %q", reason)
		}
		if banned, _, err := userDB.GetUserBanStatus(101); err != nil || banned {
			t.Fatalf("user 101 should be active: %v %v", banned, err)
		}
		if err := userDB.SetUserBanned(102, false, ""); err != nil {
			t.Fatal(err)
		}
		if banned, _, err := userDB.GetUserBanStatus(102); err != nil || banned {
			t.Fatalf("expected unbanned, got %v %v", banned, err)
		}
		// 恢复封禁供后续子测试使用
		if err := userDB.SetUserBanned(102, true, "再次封禁"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("list returns ban status and filters", func(t *testing.T) {
		items, err := userDB.ListManagementUsers(model.ManagementUserListFilters{Status: "banned"}, 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != 102 {
			t.Fatalf("banned filter should return only user 102, got %d items", len(items))
		}
		if items[0].Status != "banned" || items[0].BannedReason != "再次封禁" {
			t.Fatalf("status=%q reason=%q", items[0].Status, items[0].BannedReason)
		}
		if items[0].HasPassword {
			t.Fatal("user 102 has_password should be false (risk tag source)")
		}

		active, err := userDB.ListManagementUsers(model.ManagementUserListFilters{Status: "active"}, 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(active) != 1 || active[0].ID != 101 {
			t.Fatalf("active filter should return only user 101, got %d items", len(active))
		}
		if active[0].Phone != "13861026102" || active[0].IdentityType != "乡墅业主" {
			t.Fatalf("profile fields not joined: phone=%q identity=%q", active[0].Phone, active[0].IdentityType)
		}
		if !active[0].EnterpriseWechatVerified {
			t.Fatal("user 101 enterprise_wechat_verified should be true")
		}

		// 身份类型筛选
		byIdentity, err := userDB.ListManagementUsers(model.ManagementUserListFilters{IdentityType: "设计师"}, 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(byIdentity) != 0 {
			t.Fatalf("identity filter should return 0, got %d", len(byIdentity))
		}

		// 灵石区间筛选：0-10 只命中 102（余额 0）
		zero := int64(0)
		ten := int64(10)
		byStones, err := userDB.ListManagementUsers(model.ManagementUserListFilters{StonesMin: &zero, StonesMax: &ten}, 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(byStones) != 1 || byStones[0].ID != 102 {
			t.Fatalf("stones 0-10 filter should return only user 102, got %d", len(byStones))
		}

		// 管理端用户绝不进列表
		all, err := userDB.CountManagementUsers(model.ManagementUserListFilters{})
		if err != nil {
			t.Fatal(err)
		}
		if all != 2 {
			t.Fatalf("total miniprogram users = %d, want 2", all)
		}
	})

	t.Run("stats aggregates full db", func(t *testing.T) {
		stats, err := userDB.GetManagementUserStats()
		if err != nil {
			t.Fatal(err)
		}
		if stats.Total != 2 {
			t.Fatalf("Total = %d, want 2", stats.Total)
		}
		if stats.Banned != 1 {
			t.Fatalf("Banned = %d, want 1", stats.Banned)
		}
		if stats.PhoneVerified != 1 || stats.PhonePending != 1 {
			t.Fatalf("PhoneVerified=%d PhonePending=%d, want 1/1", stats.PhoneVerified, stats.PhonePending)
		}
		if stats.StonesTotal != 500 {
			t.Fatalf("StonesTotal = %d, want 500", stats.StonesTotal)
		}
		if stats.ZeroStonesRecharge != 1 {
			t.Fatalf("ZeroStonesRecharge = %d, want 1", stats.ZeroStonesRecharge)
		}
		if stats.MemberActive != 0 {
			t.Fatalf("MemberActive = %d, want 0", stats.MemberActive)
		}
		if stats.TodayNew != 2 {
			t.Fatalf("TodayNew = %d, want 2", stats.TodayNew)
		}
		_ = time.Now()
	})
}
