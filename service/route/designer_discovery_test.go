package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
)

func TestDesignerWorkPreviewsSortBeforeLimit(t *testing.T) {
	works := []gin.H{
		{"id": int64(1), "likes": int64(2), "created_at": time.Unix(1, 0), "main_tab": "template"},
		{"id": int64(2), "likes": int64(99), "created_at": time.Unix(2, 0), "main_tab": "template"},
		{"id": int64(3), "likes": int64(1), "created_at": time.Unix(3, 0), "main_tab": "template"},
		{"id": int64(4), "likes": int64(999), "created_at": time.Unix(4, 0), "main_tab": "material"},
	}
	popular := designerWorkPreviews(works, "template", false)
	latest := designerWorkPreviews(works, "template", true)
	if len(popular) != 2 || popular[0]["id"] != int64(2) || popular[1]["id"] != int64(1) {
		t.Fatalf("popular=%v", popular)
	}
	if latest[0]["id"] != int64(3) || latest[1]["id"] != int64(2) {
		t.Fatalf("latest=%v", latest)
	}
	if works[0]["id"] != int64(1) {
		t.Fatal("mutated original works")
	}
	if len(designerWorkPreviews(nil, "", false)) != 0 {
		t.Fatal("empty works")
	}
}

func TestPublicDesignerFiltersUseRealSpecialtiesBeforePagination(t *testing.T) {
	filters := publicDesignerFilters{Keyword: "搜索", MainTab: "template", Specialty: "庭院%设计", Sort: "popular"}
	where, args := buildPublicDesignerWhere(filters)
	for _, predicate := range []string{
		"u.user_type = 'miniprogram'", "COALESCE(up.designer_visible, 1) = 1", "tp.status = 'published'",
		"LOCATE(?, COALESCE(up.specialty_styles, '')) > 0", "t.status = 'published'",
	} {
		if !strings.Contains(where, predicate) {
			t.Fatalf("missing %q in %s", predicate, where)
		}
	}
	if strings.Contains(where, "庭院%设计") || args[len(args)-1] != "庭院%设计" {
		t.Fatalf("specialty must be bound literally: %s %v", where, args)
	}
	if strings.Contains(where, "LIMIT") || strings.Contains(where, "ORDER BY") {
		t.Fatalf("filters should apply before count and paging: %s", where)
	}
	_, defaultArgs := buildPublicDesignerWhere(publicDesignerFilters{})
	if len(defaultArgs) != 0 {
		t.Fatalf("default filters unexpectedly bound values: %v", defaultArgs)
	}
}

func TestPublicDesignerCandidateScopeKeepsUnpublishedDesignersAndExcludesOrdinaryOrHiddenAccounts(t *testing.T) {
	compiler, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable; database scope fixture is supplementary to query tests")
	}
	// 本地内存数据库只验证可移植的候选范围SQL，不连接业务MySQL。
	const schema = `
CREATE TABLE users (id INTEGER PRIMARY KEY, user_type TEXT, can_withdraw INTEGER DEFAULT 0, username TEXT DEFAULT '');
CREATE TABLE user_profiles (user_id INTEGER, designer_visible INTEGER, nickname TEXT DEFAULT '', avatar TEXT DEFAULT '', identity_type TEXT DEFAULT '', service_title TEXT DEFAULT '', designer_bio TEXT DEFAULT '', specialty_styles TEXT DEFAULT '');
CREATE TABLE certification_applications (user_id INTEGER, type TEXT, identity_type TEXT, status TEXT);
CREATE TABLE templates (creator_user_id INTEGER, status TEXT, main_tab TEXT, category TEXT DEFAULT '', sub_tab TEXT DEFAULT '');
INSERT INTO users (id, user_type) VALUES (1,'miniprogram'),(2,'miniprogram'),(3,'miniprogram'),(4,'miniprogram'),(5,'miniprogram'),(6,'miniprogram'),(7,'miniprogram'),(8,'miniprogram'),(9,'miniprogram'),(10,'management'),(11,'miniprogram'),(12,'miniprogram'),(13,'miniprogram'),(14,'miniprogram'),(15,'miniprogram');
UPDATE users SET can_withdraw=1 WHERE id=11;
UPDATE users SET username='设计师' || id;
INSERT INTO user_profiles (user_id, designer_visible, identity_type) VALUES (1,1,'设计师'),(9,0,'设计师'),(10,1,'设计师');
INSERT INTO user_profiles (user_id, designer_visible, service_title) VALUES (2,1,'主案设计师');
INSERT INTO user_profiles (user_id, designer_visible, designer_bio) VALUES (3,1,'乡墅项目设计与落地');
INSERT INTO user_profiles (user_id, designer_visible, specialty_styles) VALUES (4,1,'庭院设计');
UPDATE user_profiles SET avatar='https://cdn.example/avatar' || user_id || '.jpg';
INSERT INTO certification_applications VALUES (5,'designer','施工队','approved'),(6,'designer','设计师','pending_review'),(13,'enterprise','企业主','approved'),(14,'enterprise','设计师','approved');
INSERT INTO templates (creator_user_id,status,main_tab) VALUES (7,'published','template'),(9,'published','template'),(12,'draft','template'),(15,'published','scene');
`
	for _, test := range []struct{ name, category, want string }{
		{"all designers including no published works", "", "2,3,4,7,15\n5"},
		{"work category still requires matching published works", "template", "7\n1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			where, args := buildPublicDesignerWhere(publicDesignerFilters{MainTab: test.category})
			for _, arg := range args {
				where = strings.Replace(where, "?", "'"+strings.ReplaceAll(arg.(string), "'", "''")+"'", 1)
			}
			from := " FROM users u LEFT JOIN user_profiles up ON up.user_id=u.id WHERE " + where
			query := schema + "SELECT GROUP_CONCAT(id, ',') FROM (SELECT u.id" + from + " ORDER BY u.id); SELECT COUNT(*)" + from + ";"
			cmd := exec.Command(compiler, ":memory:")
			cmd.Stdin = strings.NewReader(query)
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != test.want {
				t.Fatalf("scope result=%q want=%q err=%v", out, test.want, err)
			}
		})
	}
}

func TestPublicDesignerRegionFilterUsesStoredPrefixBeforePagination(t *testing.T) {
	where, args := buildPublicDesignerWhere(publicDesignerFilters{Region: "福建省 泉州市"})
	if !strings.Contains(where, "LOCATE(?, COALESCE(up.designer_region, '')) = 1") || !reflect.DeepEqual(args, []interface{}{"福建省 泉州市"}) {
		t.Fatalf("region must use the actual stored prefix: where=%s args=%v", where, args)
	}
	where, _ = buildPublicDesignerWhere(publicDesignerFilters{Region: "  "})
	if strings.Contains(where, "designer_region") {
		t.Fatalf("all regions must retain unfilled profiles: %s", where)
	}
}

func TestPublicDesignerSortWhitelistsPublishedWorkMetrics(t *testing.T) {
	for _, test := range []struct{ sort, want string }{
		{"popular", "SUM(ts.like_count)"}, {"latest", "MAX(ts.created_at)"},
		{"comprehensive", "u.id DESC"}, {"popular; DROP TABLE users", "u.id DESC"},
	} {
		order := publicDesignerOrderBy(test.sort)
		if !strings.Contains(order, test.want) || strings.Contains(order, "DROP") {
			t.Fatalf("sort %q produced %s", test.sort, order)
		}
		if test.sort == "popular" || test.sort == "latest" {
			if !strings.Contains(order, "ts.creator_user_id = u.id AND ts.status = 'published'") || !strings.HasSuffix(order, "u.id DESC") {
				t.Fatalf("sort must exclude drafts with stable ties: %s", order)
			}
		}
	}
}

func TestPublicDesignerSpecialtyOptionsOnlyUseStoredValues(t *testing.T) {
	got := publicDesignerSpecialtyOptions([]string{"专业领域：庭院设计，乡墅设计", "庭院设计;新中式/现代风格", "", "专业领域：待完善", "未填写", " 乡墅设计、现代风格 "})
	want := []string{"乡墅设计", "庭院设计", "新中式", "现代风格"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("options=%v want=%v", got, want)
	}
}

type publicDesignerQueryConnector struct {
	queries []string
	fail    string
}
type publicDesignerQueryConn struct{ state *publicDesignerQueryConnector }
type publicDesignerQueryRows struct {
	values [][]driver.Value
	index  int
	err    error
}

func (s *publicDesignerQueryConnector) Connect(context.Context) (driver.Conn, error) {
	return &publicDesignerQueryConn{s}, nil
}
func (*publicDesignerQueryConnector) Driver() driver.Driver          { return materialScopeDriver{} }
func (*publicDesignerQueryConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*publicDesignerQueryConn) Close() error                        { return nil }
func (*publicDesignerQueryConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c *publicDesignerQueryConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.queries = append(c.state.queries, query)
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("designer database reads must have a deadline")
	}
	if strings.HasPrefix(query, "SELECT DISTINCT") {
		if c.state.fail == "options" {
			return nil, errors.New("options unavailable")
		}
		if len(args) != 0 || strings.Contains(query, "LIMIT") || strings.Contains(query, "LOCATE") || !strings.Contains(query, "COALESCE(up.designer_visible, 1) = 1") || !strings.Contains(query, "tp.status = 'published'") {
			return nil, errors.New("specialty options must cover all public designers")
		}
		return &publicDesignerQueryRows{values: [][]driver.Value{{"庭院设计，乡墅设计"}, {"乡墅设计"}}}, nil
	}
	if !strings.Contains(query, "LOCATE(?, COALESCE(up.specialty_styles, '')) > 0") || args[0].Value != "庭院设计" {
		return nil, errors.New("specialty must reach both count and list query")
	}
	if strings.HasPrefix(query, "SELECT COUNT(*)") {
		if c.state.fail == "count" {
			return nil, errors.New("count unavailable")
		}
		return &publicDesignerQueryRows{values: [][]driver.Value{{int64(21)}}}, nil
	}
	if c.state.fail == "list" {
		return nil, errors.New("list unavailable")
	}
	if !strings.Contains(query, "SUM(ts.like_count)") || !strings.HasSuffix(query, "LIMIT ? OFFSET ?") || args[len(args)-2].Value != int64(10) || args[len(args)-1].Value != int64(10) {
		return nil, errors.New("sort must precede second page limit")
	}
	rows := &publicDesignerQueryRows{values: [][]driver.Value{{int64(9)}, {int64(8)}}}
	if c.state.fail == "rows" {
		rows.err = errors.New("row stream interrupted")
	}
	return rows, nil
}
func (*publicDesignerQueryRows) Columns() []string { return []string{"value"} }
func (*publicDesignerQueryRows) Close() error      { return nil }
func (r *publicDesignerQueryRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		if r.err != nil {
			return r.err
		}
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func TestPublicDesignerSpecialtyOptionsIgnorePaginationAndFilters(t *testing.T) {
	db := sql.OpenDB(&publicDesignerQueryConnector{})
	t.Cleanup(func() { db.Close() })
	options, err := listPublicDesignerSpecialties(&model.UserModel{DB: db})
	if err != nil || !reflect.DeepEqual(options, []string{"乡墅设计", "庭院设计"}) {
		t.Fatalf("options=%v err=%v", options, err)
	}
}

func TestPublicDesignerReadFailuresDoNotReturnPartialResults(t *testing.T) {
	for _, failure := range []string{"count", "list", "rows", "options"} {
		t.Run(failure, func(t *testing.T) {
			db := sql.OpenDB(&publicDesignerQueryConnector{fail: failure})
			t.Cleanup(func() { db.Close() })
			if failure == "options" {
				if options, err := listPublicDesignerSpecialties(&model.UserModel{DB: db}); err == nil || options != nil {
					t.Fatalf("failure returned options=%v err=%v", options, err)
				}
				return
			}
			ids, total, err := listPublicDesignerUserIDsWithFilters(&model.UserModel{DB: db}, publicDesignerFilters{Specialty: "庭院设计", Sort: "popular"}, 10, 10)
			if err == nil || ids != nil || total != 0 {
				t.Fatalf("failure returned partial ids=%v total=%d err=%v", ids, total, err)
			}
		})
	}
}

func TestPublicDesignerPaginationUsesFilteredCountAndGlobalSort(t *testing.T) {
	state := &publicDesignerQueryConnector{}
	db := sql.OpenDB(state)
	t.Cleanup(func() { db.Close() })
	ids, total, err := listPublicDesignerUserIDsWithFilters(&model.UserModel{DB: db}, publicDesignerFilters{Specialty: "庭院设计", Sort: "popular"}, 10, 10)
	if err != nil || total != 21 || !reflect.DeepEqual(ids, []int64{9, 8}) || len(state.queries) != 2 {
		t.Fatalf("ids=%v total=%d queries=%d err=%v", ids, total, len(state.queries), err)
	}
}
