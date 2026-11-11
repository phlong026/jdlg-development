package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/tjfoc/gmsm/sm4"
	"service/function"
	"service/model"
	"service/tracking"
)

type profileGateAuthConnector struct {
	profile miniprogramProfile
	err     error
}

func (c profileGateAuthConnector) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (c profileGateAuthConnector) Driver() driver.Driver                        { return c }
func (c profileGateAuthConnector) Open(string) (driver.Conn, error)             { return c, nil }
func (c profileGateAuthConnector) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}
func (c profileGateAuthConnector) Close() error { return nil }
func (c profileGateAuthConnector) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}
func (c profileGateAuthConnector) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(query, "SELECT 1 FROM users"):
		return &profileGateAuthRows{columns: []string{"1"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(query, "FROM users"):
		// No merge record means CodeSessionRedisModel keeps the session user ID.
		return &profileGateAuthRows{columns: []string{"user_type", "merge_status", "merged_to_user_id"}}, nil
	case strings.Contains(query, "FROM user_profiles"):
		if c.err != nil {
			return nil, c.err
		}
		return &profileGateAuthRows{
			columns: []string{"nickname", "avatar"},
			values:  [][]driver.Value{{c.profile.Nickname, c.profile.Avatar}},
		}, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func TestVerifyProjectPlaybackActorAppliesProfileRuleByShareKind(t *testing.T) {
	db := sql.OpenDB(profileGateAuthConnector{profile: miniprogramProfile{Nickname: "用户"}})
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := verifyProjectPlaybackActor(ctx, db, 101, sharedVideoNote); err != nil {
		t.Fatalf("nickname-only actor rejected: %v", err)
	}
	err := verifyProjectPlaybackActor(ctx, db, 101, sharedVideoPackage)
	var trackingError *tracking.Error
	if !errors.As(err, &trackingError) || trackingError.Status != http.StatusPreconditionRequired {
		t.Fatalf("package missing avatar error = %T %v", err, err)
	}

	db = sql.OpenDB(profileGateAuthConnector{profile: miniprogramProfile{Avatar: "https://cdn.example.com/avatar.png"}})
	defer db.Close()
	err = verifyProjectPlaybackActor(ctx, db, 101, sharedVideoNote)
	if !errors.As(err, &trackingError) || trackingError.Status != http.StatusPreconditionRequired {
		t.Fatalf("missing nickname error = %T %v", err, err)
	}
}

type profileGateAuthRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *profileGateAuthRows) Columns() []string { return r.columns }
func (r *profileGateAuthRows) Close() error      { return nil }
func (r *profileGateAuthRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func signedProfileGateRequest(t *testing.T, path, token, deviceID string) *http.Request {
	t.Helper()
	signature := function.GenerateTokenSignature(token)
	sin, err := function.GenerateSin(nil)
	if err != nil {
		t.Fatal(err)
	}
	md5Signature, err := function.GenerateMD5Signature(sin, signature, path)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	pass, err := function.GeneratePass(sin, md5Signature, deviceID, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	tm, err := sm4.Sm4Ecb([]byte("0123456789abcdef"), []byte(timestamp+base64.StdEncoding.EncodeToString([]byte(path))), true)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for key, value := range map[string]string{
		"token": token, "token-signature": signature, "sin": sin, "md5-signature": md5Signature,
		"pass": pass, "tm": base64.StdEncoding.EncodeToString(tm),
	} {
		request.Header.Set(key, value)
	}
	return request
}

func TestMiniprogramProfileGateUsesRealTokenAuthenticationChain(t *testing.T) {
	if err := function.InitCrypto("", "", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/miniprogram/templates/9"
	tests := []struct {
		name             string
		profile          miniprogramProfile
		profileErr       error
		mutateRedis      func(*miniredis.Miniredis, string)
		mutateRequest    func(*http.Request)
		wantStatus       int
		wantHandlerCalls int
	}{
		{name: "complete profile", profile: miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar.png"}, wantStatus: http.StatusOK, wantHandlerCalls: 1},
		{name: "invalid signature", profile: miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar.png"}, mutateRequest: func(r *http.Request) { r.Header.Set("token-signature", "invalid") }, wantStatus: http.StatusUnauthorized},
		{name: "expired session", profile: miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar.png"}, mutateRedis: func(mr *miniredis.Miniredis, sessionID string) {
			mr.SetTTL("code_session:session:"+sessionID, time.Second)
			mr.FastForward(2 * time.Second)
		}, wantStatus: http.StatusUnauthorized},
		{name: "missing avatar", profile: miniprogramProfile{Nickname: "用户"}, wantStatus: http.StatusPreconditionRequired},
		{name: "profile query failure", profileErr: errors.New("profile database unavailable"), wantStatus: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := sql.OpenDB(profileGateAuthConnector{profile: test.profile, err: test.profileErr})
			defer db.Close()
			mr := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			defer redisClient.Close()

			session := model.CodeSession{SessionID: "profile-gate-auth-session", DeviceID: "profile-gate-device", UserID: 41}
			rawSession, err := json.Marshal(session)
			if err != nil {
				t.Fatal(err)
			}
			if err := redisClient.HSet(context.Background(), "code_session:session:"+session.SessionID, "data", rawSession).Err(); err != nil {
				t.Fatal(err)
			}
			if test.mutateRedis != nil {
				test.mutateRedis(mr, session.SessionID)
			}
			token, err := function.GenerateToken(session.SessionID)
			if err != nil {
				t.Fatal(err)
			}

			handlerCalls := 0
			router := gin.New()
			v1 := router.Group("/api/v1")
			v1.Use(
				miniprogramProfileAuthentication(TokenAuthRequired(model.NewCodeSessionRedisModel(db, redisClient))),
				miniprogramProfileCompleteness(miniprogramProfileLoaderFromModel(model.NewUserProfileModel(db))),
			)
			v1.GET("/miniprogram/templates/9", func(c *gin.Context) {
				handlerCalls++
				c.JSON(http.StatusOK, gin.H{"code": 0})
			})
			request := signedProfileGateRequest(t, path, token, session.DeviceID)
			if test.mutateRequest != nil {
				test.mutateRequest(request)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus || handlerCalls != test.wantHandlerCalls {
				t.Fatalf("status=%d handler_calls=%d body=%s", response.Code, handlerCalls, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != "private,no-store" {
				t.Fatalf("cache control = %q", got)
			}
		})
	}
}
