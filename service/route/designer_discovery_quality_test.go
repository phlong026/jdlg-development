package route

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"service/model"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const publicDesignerQualitySchema = `
CREATE TABLE users (id INTEGER PRIMARY KEY, user_type TEXT, username TEXT);
CREATE TABLE user_profiles (user_id INTEGER, designer_visible INTEGER DEFAULT 1, identity_type TEXT DEFAULT '', service_title TEXT DEFAULT '', designer_bio TEXT DEFAULT '', specialty_styles TEXT DEFAULT '', nickname TEXT DEFAULT '', avatar TEXT DEFAULT '');
CREATE TABLE certification_applications (user_id INTEGER, type TEXT, identity_type TEXT, status TEXT);
CREATE TABLE templates (creator_user_id INTEGER, status TEXT, main_tab TEXT, category TEXT DEFAULT '', sub_tab TEXT DEFAULT '', like_count INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO users VALUES (1,'miniprogram','wx_opaque_1'),(2,'miniprogram','wx_opaque_2'),(3,'miniprogram','wx_opaque_3'),(4,'miniprogram','wx_opaque_4'),(5,'miniprogram','wx_opaque_5'),(6,'miniprogram','wx_opaque_6'),(7,'miniprogram','wx_opaque_7'),(8,'miniprogram','wx_opaque_8'),(9,'miniprogram','wx_opaque_9'),(10,'management','后台'),(11,'miniprogram','wx_opaque_11'),(12,'miniprogram','wx_opaque_12'),(13,'miniprogram','wx_opaque_13'),(14,'miniprogram','wx_opaque_14'),(15,'miniprogram','陈设计'),(16,'miniprogram','wx_opaque_16');
INSERT INTO user_profiles (user_id,identity_type,nickname,avatar,designer_bio) VALUES
 (1,'设计师','林设计','https://cdn.example/avatar1.jpg','乡墅项目设计与落地'),
 (2,'设计师','','',''),
 (3,'设计师','wx_opaque_3','',''),
 (4,'设计师','周设计','https://cdn.example/avatar4.jpg',''),
 (5,'设计师','待完善','https://cdn.example/avatar5.jpg','乡墅设计'),
 (6,'设计师','陈设计','https://cdn.example/avatar6.jpg','个人信息：室内设计师'),
 (7,'设计师','有作品设计师','',''),
 (8,'设计师','wx_opaque_8','https://cdn.example/avatar8.jpg','乡墅设计'),
 (9,'设计师','隐藏设计师','https://cdn.example/avatar9.jpg','乡墅设计'),
 (10,'设计师','后台','https://cdn.example/avatar10.jpg','乡墅设计'),
 (11,'设计师','旧临时头像','wxfile://avatar.jpg','乡墅设计'),
 (12,'设计师','默认头像','https://cdn.example/assets/images/default-avatar.png','乡墅设计'),
 (13,'乡墅业主','普通用户','https://cdn.example/avatar13.jpg',''),
 (14,'设计师','只有草稿','https://cdn.example/avatar14.jpg',''),
 (16,'设计师','完整未发布','https://cdn.example/avatar16.jpg','庭院设计与实施');
UPDATE user_profiles SET designer_visible=0 WHERE user_id=9;
UPDATE user_profiles SET service_title='待完善',specialty_styles='专业领域：待完善' WHERE user_id=6;
INSERT INTO certification_applications VALUES (2,'designer','设计师','approved'),(16,'designer','设计师','approved');
INSERT INTO templates (creator_user_id,status,main_tab,like_count,created_at) VALUES
 (7,'published','template',2,'2026-10-01'),(8,'published','template',100,'2026-10-01'),(9,'published','template',100,'2026-10-01'),
 (10,'published','template',100,'2026-10-01'),(14,'draft','template',9999,'2026-11-10'),(15,'published','template',1,'2026-10-01');
`

func runDesignerQualitySQL(t *testing.T, query string) string {
	t.Helper()
	binary, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable for supplementary SQL fixture")
	}
	command := exec.Command(binary, ":memory:")
	command.Stdin = strings.NewReader(query)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("SQL fixture: %s: %v", output, err)
	}
	return strings.TrimSpace(string(output))
}

func TestPublicDesignerQualityExcludesSystemAndIncompleteProfilesBeforeCounting(t *testing.T) {
	where, args := buildPublicDesignerWhere(publicDesignerFilters{})
	if len(args) != 0 {
		t.Fatal("unexpected default arguments", args)
	}
	from := " FROM users u LEFT JOIN user_profiles up ON up.user_id=u.id WHERE " + where
	got := runDesignerQualitySQL(t, publicDesignerQualitySchema+"SELECT GROUP_CONCAT(id, ',') FROM (SELECT u.id"+from+" ORDER BY u.id); SELECT COUNT(*)"+from+";")
	if got != "1,7,15,16\n4" {
		t.Fatalf("system/unfilled accounts entered public list or count: %q", got)
	}
}

func TestPublicDesignerComprehensiveSortFollowsSelectedBusinessOrder(t *testing.T) {
	// 草稿不参与排名；认证、点赞和时间相同时由ID稳定打平。
	const schema = `
CREATE TABLE users(id INTEGER PRIMARY KEY);
CREATE TABLE templates(creator_user_id INTEGER,status TEXT,like_count INTEGER,created_at TEXT);
CREATE TABLE certification_applications(user_id INTEGER,status TEXT);
INSERT INTO users VALUES(1),(2),(3),(4),(5),(6),(7),(8);
INSERT INTO certification_applications VALUES(1,'approved'),(3,'approved'),(4,'approved'),(6,'approved'),(7,'pending_review'),(8,'approved');
INSERT INTO templates VALUES(2,'published',999,'2026-11-01'),(3,'published',5,'2026-11-01'),(4,'published',9,'2026-10-01'),(5,'published',999,'2026-11-02'),(6,'published',5,'2026-11-03'),(7,'published',0,'2026-01-01'),(1,'draft',99999,'2026-11-10'),(8,'published',5,'2026-11-03');
`
	for _, sort := range []string{"comprehensive", "invalid; DROP TABLE users"} {
		got := runDesignerQualitySQL(t, schema+"SELECT GROUP_CONCAT(id, ',') FROM (SELECT u.id FROM users u ORDER BY "+publicDesignerOrderBy(sort)+");")
		if got != "4,8,6,3,5,2,7,1" {
			t.Fatalf("sort %q got=%s", sort, got)
		}
	}
}

func TestDesignerPublicDisplayNameNeverLeaksGeneratedAccountNames(t *testing.T) {
	for _, test := range []struct{ username, nickname, want string }{
		{"wx_opaque_openid", "林设计", "林设计"},
		{"wx_opaque_openid", "", "设计师"},
		{"wx_opaque_openid", "wx_opaque_openid", "设计师"},
		{"wx_opaque_openid", "WX_opaque_openid", "设计师"},
		{"陈工", "待完善", "陈工"},
		{" 李设计 ", "", "李设计"},
		{"wx_opaque_openid", "匿名设计师", "设计师"},
	} {
		if got := designerPublicDisplayName(test.username, test.nickname); got != test.want {
			t.Fatalf("public name=%q want=%q", got, test.want)
		}
	}
}

func TestPublicDesignerHomepageProjectionExcludesPrivateBusinessMetrics(t *testing.T) {
	payload := gin.H{
		"public_visible": true,
		"profile": gin.H{
			"user_id": int64(7), "name": "林设计", "bio_text": "证件说明：身份证号", "cert_status": "approved",
		},
		"public_profile": gin.H{
			"user_id": int64(7), "name": "林设计", "bio_text": "个人信息：专注乡墅设计",
		},
		"stats": gin.H{"total_works": int64(3), "total_orders": int64(9), "month_orders": int64(2), "total_earnings": int64(888)},
		"service_config": gin.H{
			"service_type": "乡墅设计", "created_at_text": "2026年1月2日", "fee": int64(99),
			"service_intro": "设计服务", "service_enabled": true,
			"total_earnings": int64(888), "month_earnings": int64(66), "total_orders": int64(9),
		},
		"public_service_config": gin.H{
			"service_type": "乡墅设计", "fee": int64(99), "service_intro": "设计服务", "service_enabled": true,
		},
		"review_summary": gin.H{"positive_count": int64(2), "negative_count": int64(0)},
		"works":          []gin.H{{"id": int64(4)}},
		"reviews": []gin.H{{
			"id": int64(5), "name": "评价人", "avatar": "https://example.test/avatar.jpg",
			"score": 5, "content": "服务很好", "sentiment": "positive",
			"related_title": "客户私密项目", "related_template_id": int64(99),
		}},
	}

	public := publicDesignerHomepagePayload(payload)
	stats, ok := public["stats"].(gin.H)
	if !ok || stats["total_works"] != int64(3) {
		t.Fatalf("public stats = %#v", public["stats"])
	}
	for _, key := range []string{"total_orders", "month_orders", "total_earnings", "month_earnings"} {
		if _, exists := stats[key]; exists {
			t.Fatalf("private stat %s leaked: %#v", key, stats)
		}
	}
	service, ok := public["service_config"].(gin.H)
	if !ok || service["fee"] != int64(99) || service["service_enabled"] != true {
		t.Fatalf("public service = %#v", public["service_config"])
	}
	for _, key := range []string{"total_orders", "total_earnings", "month_earnings"} {
		if _, exists := service[key]; exists {
			t.Fatalf("private service metric %s leaked: %#v", key, service)
		}
	}
	if _, exists := service["created_at_text"]; exists {
		t.Fatalf("account or certification date leaked: %#v", service)
	}
	reviews, ok := public["reviews"].([]gin.H)
	if !ok || len(reviews) != 1 || reviews[0]["content"] != "服务很好" {
		t.Fatalf("public reviews = %#v", public["reviews"])
	}
	if reviews[0]["name"] != "匿名用户" || reviews[0]["avatar"] != "" {
		t.Fatalf("reviewer identity leaked: %#v", reviews[0])
	}
	if _, exists := reviews[0]["related_title"]; exists {
		t.Fatalf("private order context leaked: %#v", reviews[0])
	}
	profile, ok := public["profile"].(gin.H)
	if !ok || profile["bio_text"] != "个人信息：专注乡墅设计" {
		t.Fatalf("public profile = %#v", public["profile"])
	}
	for _, key := range []string{"cert_status", "designer_visible"} {
		if _, exists := profile[key]; exists {
			t.Fatalf("internal profile field %s leaked: %#v", key, profile)
		}
	}
	if strings.Contains(profile["bio_text"].(string), "证件") {
		t.Fatalf("certification remark leaked: %#v", profile)
	}
}

func TestDesignerHomepageAdmissionMatchesPublicDirectoryBoundary(t *testing.T) {
	ordinary := &model.User{UserType: "miniprogram", Username: "普通用户"}
	pendingOnly := &model.UserProfile{Nickname: "待审设计师", DesignerVisible: true}
	if isDesignerPublicVisible(ordinary, nil, 0) {
		t.Fatal("ordinary account without a public designer profile became enumerable")
	}
	if isDesignerPublicVisible(ordinary, pendingOnly, 0) {
		t.Fatal("a name or pending certification alone became a public designer homepage")
	}
	hidden := &model.UserProfile{
		Nickname: "隐藏设计师", Avatar: "https://cdn.example/avatar.jpg", DesignerBio: "乡墅设计", DesignerVisible: false,
	}
	if isDesignerPublicVisible(ordinary, hidden, 0) {
		t.Fatal("designer_visible=false became a public designer homepage")
	}
	ready := &model.UserProfile{
		Nickname: "公开设计师", Avatar: "https://cdn.example/avatar.jpg", DesignerBio: "乡墅设计", DesignerVisible: true,
	}
	if !isDesignerPublicVisible(ordinary, ready, 0) {
		t.Fatal("a complete public designer profile was rejected")
	}
	creator := &model.User{UserType: "miniprogram", Username: "作品设计师"}
	if !isDesignerPublicVisible(creator, nil, 1) {
		t.Fatal("a named creator with a published work was rejected")
	}
}

func TestPublicDesignerHomepageReturns404ForNonPublicAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		user    *model.User
		profile *model.UserProfile
		works   int64
	}{
		{name: "ordinary", user: &model.User{UserType: "miniprogram", Username: "普通用户"}},
		{name: "pending-only", user: &model.User{UserType: "miniprogram", Username: "待审用户"}, profile: &model.UserProfile{Nickname: "待审设计师", DesignerVisible: true}},
		{name: "hidden", user: &model.User{UserType: "miniprogram", Username: "隐藏设计师"}, profile: &model.UserProfile{Nickname: "隐藏设计师", Avatar: "https://cdn.example/avatar.jpg", DesignerBio: "乡墅设计", DesignerVisible: false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/designers/:id/homepage", func(c *gin.Context) {
				servePublicDesignerHomepage(c, func(int64) (gin.H, error) {
					return gin.H{"public_visible": isDesignerPublicVisible(test.user, test.profile, test.works)}, nil
				})
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/designers/9/homepage", nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPublicDesignerQualityUsesSameScopeForAllPagesAndTotal(t *testing.T) {
	where, _ := buildPublicDesignerWhere(publicDesignerFilters{})
	from := " FROM users u LEFT JOIN user_profiles up ON up.user_id=u.id WHERE " + where
	query := publicDesignerQualitySchema + "SELECT u.id" + from + " ORDER BY " + publicDesignerOrderBy("comprehensive") + " LIMIT 2 OFFSET 0; SELECT u.id" + from + " ORDER BY " + publicDesignerOrderBy("comprehensive") + " LIMIT 2 OFFSET 2; SELECT COUNT(*)" + from + ";"
	if got := runDesignerQualitySQL(t, query); got != "7\n15\n16\n1\n4" {
		t.Fatalf("pagination includes empty accounts or total mismatches: %q", got)
	}
}
