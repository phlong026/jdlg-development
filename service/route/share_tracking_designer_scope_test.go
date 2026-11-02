package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

type designerScopeCertification struct{ kind, status, identity string }
type designerScopeGrant struct {
	owner                 string
	enabled, manage, view bool
}
type designerScopeAccount struct {
	userType, status string
	certifications   []designerScopeCertification
	grants           []designerScopeGrant
}
type designerScopeConnector struct {
	accounts map[int64]designerScopeAccount
	queries  int
	shares   map[int64][]string
}
type designerScopeConn struct{ state *designerScopeConnector }

func (s *designerScopeConnector) Connect(context.Context) (driver.Conn, error) {
	return designerScopeConn{s}, nil
}
func (*designerScopeConnector) Driver() driver.Driver         { return materialScopeDriver{} }
func (designerScopeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (designerScopeConn) Close() error                        { return nil }
func (designerScopeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c designerScopeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.queries++
	query = strings.Join(strings.Fields(query), " ")
	viewOnly := strings.Contains(query, "t.can_view=TRUE")
	for _, predicate := range []string{"t.user_id=?", "t.tenant_id=?", "t.enabled=TRUE", "u.user_type='miniprogram'", "COALESCE(u.status,'active')='active'"} {
		if !strings.Contains(query, predicate) {
			return nil, fmt.Errorf("missing account predicate: %s", predicate)
		}
	}
	if !viewOnly || strings.Contains(query, "certification_applications ca") {
		for _, predicate := range []string{"UNION", "u.id=?", "EXISTS", "certification_applications ca", "ca.user_id=u.id", "ca.type='designer'", "ca.status='approved'", "TRIM(ca.identity_type)='设计师'"} {
			if !strings.Contains(query, predicate) {
				return nil, fmt.Errorf("missing certification predicate: %s", predicate)
			}
		}
		if strings.Count(query, "u.user_type='miniprogram'") != 2 || strings.Count(query, "COALESCE(u.status,'active')='active'") != 2 || !viewOnly && (len(args) != 3 || args[2].Value != args[0].Value) {
			return nil, errors.New("personal scope must use the same active mini-account")
		}
	}
	if viewOnly {
		for _, predicate := range []string{"r.tenant_id=?", "r.kind='share'", "r.visitor_key=?", "r.visitor_key=''", "'$.sharer_id'", "NOT EXISTS", "denied.user_id=u.id", "denied.tenant_id=?", "denied.owner_id=candidate.owner_id", "denied.enabled=FALSE OR denied.can_view=FALSE"} {
			if !strings.Contains(query, predicate) {
				return nil, fmt.Errorf("missing self-share boundary: %s", predicate)
			}
		}
		if len(args) != 8 || args[2].Value != args[0].Value || args[6].Value != args[0].Value || args[3].Value != args[1].Value || args[7].Value != args[1].Value || args[4].Value != fmt.Sprintf("u:%d", args[0].Value) || args[5].Value != args[4].Value {
			return nil, errors.New("self-share scope must bind the current actor and tenant")
		}
	}
	if strings.Contains(query, "can_withdraw") || strings.Contains(query, "user_profiles") {
		return nil, errors.New("self-declared identity is not certification authority")
	}
	actor, tenant := args[0].Value.(int64), args[1].Value.(string)
	account, found := c.state.accounts[actor]
	rows := &materialScopeRows{}
	if !found || account.userType != "miniprogram" || account.status != "active" {
		return rows, nil
	}
	for _, grant := range account.grants {
		if tenant == "tenant-a" && grant.enabled && (viewOnly && grant.view || !viewOnly && grant.manage) {
			rows.values = append(rows.values, grant.owner)
		}
	}
	if !viewOnly || strings.Contains(query, "certification_applications ca") {
		for _, certification := range account.certifications {
			if certification.kind == "designer" && certification.status == "approved" && strings.TrimSpace(certification.identity) == "设计师" {
				owners := []string{fmt.Sprintf("designer-%d", actor)}
				if viewOnly {
					owners = append(owners, c.state.shares[actor]...)
				}
				for _, owner := range owners {
					denied := false
					if viewOnly {
						for _, grant := range account.grants {
							if grant.owner == owner && (!grant.enabled || !grant.view) {
								denied = true
							}
						}
					}
					if !denied {
						rows.values = append(rows.values, owner)
					}
				}
				break
			}
		}
	}
	return rows, nil
}

func designerScopeDB(t *testing.T, accounts map[int64]designerScopeAccount) (*sql.DB, *designerScopeConnector) {
	t.Helper()
	state := &designerScopeConnector{accounts: accounts}
	db := sql.OpenDB(state)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db, state
}

func TestCertifiedDesignerMaterialScopeAuthorityMatrix(t *testing.T) {
	approved := designerScopeCertification{"designer", "approved", "设计师"}
	cases := []struct {
		name    string
		account designerScopeAccount
		owners  []string
	}{
		{"approved designer", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{approved}, nil}, []string{"designer-41"}},
		{"trim approved identity", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "approved", " 设计师 "}}, nil}, []string{"designer-41"}},
		{"approved survives later pending application", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{approved, {"designer", "pending_review", "设计师"}}, nil}, []string{"designer-41"}},
		{"self-declared only", designerScopeAccount{"miniprogram", "active", nil, nil}, nil},
		{"pending review", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "pending_review", "设计师"}}, nil}, nil},
		{"pending payment", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "pending_payment", "设计师"}}, nil}, nil},
		{"rejected", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "rejected", "设计师"}}, nil}, nil},
		{"enterprise", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"enterprise", "approved", "设计师"}}, nil}, nil},
		{"construction team", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "approved", "施工队"}}, nil}, nil},
		{"empty historical identity", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "approved", ""}}, nil}, nil},
		{"banned designer", designerScopeAccount{"miniprogram", "banned", []designerScopeCertification{approved}, nil}, nil},
		{"management account", designerScopeAccount{"management", "active", []designerScopeCertification{approved}, nil}, nil},
		{"explicit non-designer", designerScopeAccount{"miniprogram", "active", nil, []designerScopeGrant{{"organization-a", true, true, false}}}, []string{"organization-a"}},
		{"revoked certification with independent grant", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "rejected", "设计师"}}, []designerScopeGrant{{"organization-a", true, true, false}}}, []string{"organization-a"}},
		{"banned explicit operator", designerScopeAccount{"miniprogram", "banned", nil, []designerScopeGrant{{"organization-a", true, true, false}}}, nil},
		{"view grant alone", designerScopeAccount{"miniprogram", "active", nil, []designerScopeGrant{{"organization-a", true, false, true}}}, nil},
		{"disabled grant", designerScopeAccount{"miniprogram", "active", nil, []designerScopeGrant{{"organization-a", false, true, false}}}, nil},
		{"merge and deduplicate explicit grants", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{approved}, []designerScopeGrant{{"organization-a", true, true, false}, {"designer-41", true, true, false}}}, []string{"designer-41", "organization-a"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			db, state := designerScopeDB(t, map[int64]designerScopeAccount{41: test.account})
			scope, err := loadMiniTrackingMaterialScope(context.Background(), db, "tenant-a", 41)
			if state.queries != 1 {
				t.Fatalf("scope must use one source query, got %d", state.queries)
			}
			if len(test.owners) == 0 {
				var denied *tracking.Error
				if !errors.As(err, &denied) || denied.Status != 403 {
					t.Fatalf("want denied scope, got %+v error=%v", scope, err)
				}
				return
			}
			sort.Strings(scope.Owners)
			if err != nil || !reflect.DeepEqual(scope.Owners, test.owners) || scope.Actor != 41 || scope.Tenant != "tenant-a" || !scope.Manage || scope.View || scope.CRM || scope.Delete || scope.Rules {
				t.Fatalf("unexpected material scope: %+v error=%v", scope, err)
			}
		})
	}
}

func TestCertifiedDesignerOwnSharesAuthority(t *testing.T) {
	approved := designerScopeCertification{"designer", "approved", "设计师"}
	cases := []struct {
		name    string
		account designerScopeAccount
		owners  []string
	}{
		{"approved designer", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{approved}, nil}, []string{"company", "designer-41"}},
		{"self declared", designerScopeAccount{"miniprogram", "active", nil, nil}, nil},
		{"pending", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "pending_review", "设计师"}}, nil}, nil},
		{"revoked certification", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "rejected", "设计师"}}, nil}, nil},
		{"other identity", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "approved", "施工队"}}, nil}, nil},
		{"banned", designerScopeAccount{"miniprogram", "banned", []designerScopeCertification{approved}, nil}, nil},
		{"management", designerScopeAccount{"management", "active", []designerScopeCertification{approved}, nil}, nil},
		{"explicit owner denial", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{approved}, []designerScopeGrant{{"company", true, true, false}}}, []string{"designer-41"}},
		{"explicit owner disabled", designerScopeAccount{"miniprogram", "active", []designerScopeCertification{approved}, []designerScopeGrant{{"company", false, true, true}}}, []string{"designer-41"}},
		{"independent explicit view", designerScopeAccount{"miniprogram", "active", nil, []designerScopeGrant{{"organization", true, false, true}}}, []string{"organization"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			db, state := designerScopeDB(t, map[int64]designerScopeAccount{41: test.account})
			state.shares = map[int64][]string{41: {"company"}}
			scope, err := loadMiniTrackingMyScope(context.Background(), db, "tenant-a", 41)
			if len(test.owners) == 0 {
				if !errors.Is(err, errMySharesViewDenied) {
					t.Fatalf("expected denial: %+v %v", scope, err)
				}
				return
			}
			sort.Strings(scope.Owners)
			if err != nil || !reflect.DeepEqual(scope.Owners, test.owners) || !scope.View || scope.Manage || scope.CRM || scope.Delete || scope.Rules || scope.Actor != 41 {
				t.Fatalf("wrong self-only scope: %+v %v", scope, err)
			}
		})
	}
}

func TestCertifiedDesignerIsolationAndLiveProjectPermissionRevocation(t *testing.T) {
	account := designerScopeAccount{"miniprogram", "active", []designerScopeCertification{{"designer", "approved", "设计师"}}, nil}
	db, state := designerScopeDB(t, map[int64]designerScopeAccount{41: account, 42: account})
	ctx := context.Background()
	designer, err := loadMiniTrackingMaterialScope(ctx, db, "tenant-a", 41)
	if err != nil {
		t.Fatal(err)
	}
	other, err := loadMiniTrackingMaterialScope(ctx, db, "tenant-a", 42)
	if err != nil {
		t.Fatal(err)
	}
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "tenant-a", Enabled: true, ContentEnabled: true, Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}}})
	var notice tracking.NoticeEvent
	svc.OnNoticeEvent = func(_ context.Context, event tracking.NoticeEvent) error { notice = event; return nil }
	gin.SetMode(gin.TestMode)
	registerTrackingMaterialRoutes(gin.New().Group("/materials"), svc, &model.UserModel{DB: db}, nil)
	draft, err := svc.SaveContent(ctx, designer, "", tracking.Draft{Title: "设计师自己的内容", Body: "正文", Owner: "designer-41", Rights: "own"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SaveContent(ctx, other, draft.ID, tracking.Draft{Title: "其他设计师修改", Body: "正文", Owner: "designer-41", Rights: "own", Revision: draft.Revision}); err == nil {
		t.Fatal("another designer edited private content")
	}
	contents, err := svc.Contents(ctx, other, tracking.Filter{}, draft.ID)
	if err != nil || len(contents.Items.([]tracking.Content)) != 0 {
		t.Fatalf("another designer read private content: %+v error=%v", contents, err)
	}
	preview, err := svc.Preview(ctx, designer, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Publish(ctx, designer, draft.ID, draft.Revision, preview["preview_token"].(string), "designer-publish-1"); err != nil {
		t.Fatal(err)
	}
	ref, err := svc.CreateNativeReference(ctx, designer, tracking.NativeReferenceInput{ContentID: draft.ID, ContentVersion: 1, IdempotencyKey: "designer-reference-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.NativeReferenceTarget(ctx, other, ref.ReferenceID); err == nil {
		t.Fatal("another designer opened a private reference")
	}
	if _, err = svc.CreateNativeReference(ctx, other, tracking.NativeReferenceInput{ContentID: draft.ID, ContentVersion: 1, IdempotencyKey: "foreign-reference-1"}); err == nil {
		t.Fatal("another designer selected an unauthorized source")
	}
	project, err := svc.ProjectNoteCreateProject(ctx, designer, tracking.ProjectNoteProjectInput{Name: "个人项目", Owner: "designer-41"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteGetProject(ctx, other, project.ID); err == nil {
		t.Fatal("another designer read a private project")
	}
	if _, err = svc.ProjectNoteInviteMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	note, err := svc.ProjectNoteCreateNote(ctx, designer, project.ID, tracking.ProjectNoteDraft{Title: "方案", Body: `{"schema":1,"blocks":[{"kind":"paragraph","text":"首版"}]}`, BodyFormat: "article_v2"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share, err := svc.ProjectNoteCreateShare(ctx, designer, version.ID, svc.Now().Add(time.Hour), "designer-note-share-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteAddFeedback(ctx, share.Token, 101, "调整门厅", "designer-feedback-1"); err != nil {
		t.Fatal(err)
	}
	if eligible, err := svc.ProjectNoteNoticeEligible(ctx, notice.ID, designer.Actor, designer.Owners...); err != nil || !eligible {
		t.Fatalf("current designer feedback notice denied: eligible=%v error=%v", eligible, err)
	}
	account.certifications = []designerScopeCertification{{"designer", "rejected", "设计师"}}
	state.accounts[41] = account
	if _, err = loadMiniTrackingMaterialScope(ctx, db, "tenant-a", 41); err == nil {
		t.Fatal("revoked certification retained material scope")
	}
	if allowed, err := svc.IsContentOperator(ctx, 41, "designer-41"); err != nil || allowed {
		t.Fatalf("live operator check ignored revocation: allowed=%v error=%v", allowed, err)
	}
	if _, err = svc.ProjectNoteReadShare(ctx, share.Token, 101); err == nil {
		t.Fatal("member read survived designer certification revocation")
	}
	if _, err = svc.ProjectNoteAddFeedback(ctx, share.Token, 101, "不应被采纳", "revoked-feedback-1"); err == nil {
		t.Fatal("member feedback survived designer certification revocation")
	}
	if eligible, err := svc.ProjectNoteNoticeEligible(ctx, notice.ID, designer.Actor, designer.Owners...); err != nil || eligible {
		t.Fatalf("feedback notice survived certification revocation: eligible=%v error=%v", eligible, err)
	}
}
