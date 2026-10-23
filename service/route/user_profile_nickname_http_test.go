package route

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/tjfoc/gmsm/sm4"
	"service/function"
	"service/model"
)

// Called only by the explicitly configured, empty disposable MySQL test.
func testNicknameHTTPRoundTrip(t *testing.T, db *sql.DB, userID int64, assertReads func(string)) {
	t.Helper()
	key := []byte("0123456789abcdef") // Public test fixture, never business credentials.
	if err := function.InitCrypto("", "", base64.StdEncoding.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rc.Close()
	cs := model.CodeSession{SessionID: "nickname-http-fixture", DeviceID: "fixture-device", UserID: userID}
	rawSession, _ := json.Marshal(cs)
	if err := rc.HSet(context.Background(), "code_session:session:"+cs.SessionID, "data", rawSession).Err(); err != nil {
		t.Fatal(err)
	}
	token, err := function.GenerateToken(cs.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	profiles := model.NewUserProfileModel(db)
	RegisterUserProfileRoutes(router.Group("/api/v1/miniprogram"), model.NewCodeSessionRedisModel(db, rc), model.NewUserModel(db), profiles)
	request := func(method, suffix string, body any, want int) map[string]any {
		t.Helper()
		path := "/api/v1/miniprogram/profile" + suffix
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		signature := function.GenerateTokenSignature(token)
		sin, err := function.GenerateSin(raw)
		if err != nil {
			t.Fatal(err)
		}
		md5, err := function.GenerateMD5Signature(sin, signature, path)
		if err != nil {
			t.Fatal(err)
		}
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		pass, err := function.GeneratePass(sin, md5, cs.DeviceID, timestamp)
		if err != nil {
			t.Fatal(err)
		}
		tm, err := sm4.Sm4Ecb(key, []byte(timestamp+base64.StdEncoding.EncodeToString([]byte(path))), true)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		for k, v := range map[string]string{"Content-Type": "application/json", "token": token, "token-signature": signature, "sin": sin, "md5-signature": md5, "pass": pass, "tm": base64.StdEncoding.EncodeToString(tm)} {
			req.Header.Set(k, v)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("%s %s: HTTP%d want %d: %s", method, path, out.Code, want, out.Body.String())
		}
		var response map[string]any
		if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	for _, column := range []string{"nickname", "phone"} {
		if _, err := db.Exec("UPDATE user_profiles SET "+column+"=NULL WHERE user_id=?", userID); err != nil {
			t.Fatal(err)
		}
		request("GET", "", nil, 200)
		for i := 0; i < 2; i++ { // Repeating the same value must succeed, too.
			result := request("PUT", "/nickname", map[string]string{"nickname": "  林间访客  "}, 200)
			if result["data"].(map[string]any)["nickname"] != "林间访客" {
				t.Fatal(result)
			}
			got := request("GET", "", nil, 200)
			if got["data"].(map[string]any)["nickname"] != "林间访客" {
				t.Fatal(got)
			}
			assertReads("林间访客")
		}
	}
	request("PUT", "/nickname", map[string]string{"nickname": " \t "}, 400)
	request("PUT", "/identity", map[string]any{"identity_type": "设计师"}, 200)
	request("PUT", "/avatar", map[string]string{"avatar": "https://example.test/avatar.png"}, 200)
	assertReads("林间访客")
	if _, err := db.Exec("DELETE FROM user_profiles WHERE user_id=?", userID); err != nil {
		t.Fatal(err)
	}
	request("PUT", "/nickname", map[string]string{"nickname": "重新创建"}, 200)
	assertReads("重新创建")
	if _, err := db.Exec("ALTER TABLE user_profiles RENAME COLUMN phone TO fixture_missing_phone"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("ALTER TABLE user_profiles RENAME COLUMN fixture_missing_phone TO phone")
	request("GET", "", nil, 500)
	if _, err := profiles.GetOrCreate(userID, ""); err == nil {
		t.Fatal("read error must propagate, not create another profile")
	}
}
