package route

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"service/model"
)

// 昵称持久化的 MySQL 集成测试。
// 仅在显式提供空的一次性测试库时运行：
//
//	CREATE DATABASE jd_nickname_test; NICKNAME_TEST_DSN='user:pass@tcp(127.0.0.1:13306)/jd_nickname_test?parseTime=true' go test ./route -run TestUserProfileNicknamePersistence
func TestUserProfileNicknamePersistence(t *testing.T) {
	dsn := os.Getenv("NICKNAME_TEST_DSN")
	if dsn == "" {
		t.Skip("set NICKNAME_TEST_DSN to an isolated jd_nickname_test database")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "jd_nickname_test") {
		t.Fatal("refusing non-test database")
	}
	parsed.ParseTime = true
	db, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var tableCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()`).Scan(&tableCount); err != nil || tableCount != 0 {
		t.Fatalf("test database must be empty: %d %v", tableCount, err)
	}

	userModel := model.NewUserModel(db)
	if err := userModel.InitTable(); err != nil {
		t.Fatal(err)
	}
	profileModel := model.NewUserProfileModel(db)
	if err := profileModel.InitTable(); err != nil {
		t.Fatal(err)
	}
	if err := model.NewUserMembershipModel(db).InitTable(); err != nil {
		t.Fatal(err)
	}

	primaryUser, err := userModel.CreateOrUpdateByOpenID("nickname-test-primary-openid", "")
	if err != nil {
		t.Fatal(err)
	}
	otherUser, err := userModel.CreateOrUpdateByOpenID("nickname-test-other-openid", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profileModel.GetOrCreate(primaryUser.ID, "device-primary"); err != nil {
		t.Fatal(err)
	}
	if _, err := profileModel.GetOrCreate(otherUser.ID, "device-other"); err != nil {
		t.Fatal(err)
	}
	if err := profileModel.UpdateNickname(primaryUser.ID, "林间访客"); err != nil {
		t.Fatal(err)
	}
	if err := profileModel.UpdateNickname(otherUser.ID, "隔离用户"); err != nil {
		t.Fatal(err)
	}

	assertAllReads := func(primaryNickname string) {
		t.Helper()

		primaryProfile, err := profileModel.GetOrCreate(primaryUser.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		otherProfile, err := profileModel.GetOrCreate(otherUser.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if primaryProfile.Nickname != primaryNickname || otherProfile.Nickname != "隔离用户" {
			t.Fatalf("profile nicknames = %q/%q, want %q/%q", primaryProfile.Nickname, otherProfile.Nickname, primaryNickname, "隔离用户")
		}

		users, err := userModel.ListManagementUsers(model.ManagementUserListFilters{}, 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		managementNames := make(map[int64]string, len(users))
		for _, user := range users {
			managementNames[user.ID] = user.Nickname
		}
		if managementNames[primaryUser.ID] != primaryNickname || managementNames[otherUser.ID] != "隔离用户" {
			t.Fatalf("management nicknames = %q/%q, want %q/%q", managementNames[primaryUser.ID], managementNames[otherUser.ID], primaryNickname, "隔离用户")
		}

		profiles, err := makeTrackingProfileResolver(db)(context.Background(), []int64{primaryUser.ID, otherUser.ID})
		if err != nil {
			t.Fatal(err)
		}
		if profiles[primaryUser.ID].Nickname != primaryNickname || profiles[otherUser.ID].Nickname != "隔离用户" {
			t.Fatalf("tracking nicknames = %q/%q, want %q/%q", profiles[primaryUser.ID].Nickname, profiles[otherUser.ID].Nickname, primaryNickname, "隔离用户")
		}
	}

	assertAllReads("林间访客")
	if err := profileModel.UpdateNickname(primaryUser.ID, "山居访客"); err != nil {
		t.Fatal(err)
	}
	assertAllReads("山居访客")
}
