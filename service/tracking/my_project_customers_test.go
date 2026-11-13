package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestMyVisitorsUnifiesOnlyOwnProjectCustomerFactsWithoutPrivateBody(t *testing.T) {
	s, scope, other := projectNoteFixture(t)
	ctx := context.Background()
	project, _, _, share := seedProjectNote(t, s, scope, 101)
	if _, err := s.ProjectNoteOpenShare(ctx, share.Token, 101, "open-customer-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectNoteAddFeedback(ctx, share.Token, 101, "private feedback must not enter customer summary", "feedback-customer-001"); err != nil {
		t.Fatal(err)
	}
	list, err := s.MyVisitors(ctx, scope, scope.Actor, Filter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitorItems(t, list)
	if len(items) != 1 || items[0].Key != "u:101" {
		t.Fatalf("project customer missing from unified list: %+v", items)
	}
	detail, err := s.MyVisitorEntries(ctx, scope, scope.Actor, Filter{Visitor: "u:101", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.ProjectActivities) != 2 || detail.ProjectActivities[0].ProjectID != project.ID {
		t.Fatalf("project timeline missing: %+v", detail.ProjectActivities)
	}
	raw, _ := json.Marshal(detail)
	if strings.Contains(string(raw), "private feedback must not enter customer summary") || strings.Contains(string(raw), "方案一") {
		t.Fatal("private正文/feedback leaked into customer summary")
	}
	if _, err = s.MyVisitorEntries(ctx, other, other.Actor, Filter{Visitor: "u:101", Limit: 20}); err == nil {
		t.Fatal("same owner other designer read private project customer")
	}
	if err := s.transaction(ctx, func(r Repository) error {
		return r.Put(record("deletion", "u:101", project.Owner, s.Now(), map[string]any{}))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MyVisitorEntries(ctx, scope, scope.Actor, Filter{Visitor: "u:101", Limit: 20}); err == nil {
		t.Fatal("deleted customer facts reappeared through project activity")
	}
}

func TestMyProjectCustomerFilterRunsBeforeActivityLimit(t *testing.T) {
	s, scope, _ := projectNoteFixture(t)
	ctx := context.Background()
	_, _, _, share := seedProjectNote(t, s, scope, 101)
	if _, err := s.ProjectNoteOpenShare(ctx, share.Token, 101, "open-own-customer"); err != nil {
		t.Fatal(err)
	}
	err := s.transaction(ctx, func(r Repository) error {
		p := ProjectNoteProject{ID: "other-project-001", Owner: "owner-a", DesignerID: 8, Name: "同事项目"}
		if err := r.Put(projectNoteRecord(projectNoteProjectKind, p.ID, p.Owner, s.Now(), p, "u:8", "", p.ID, "")); err != nil {
			return err
		}
		for i := 0; i < 150; i++ {
			id := fmt.Sprintf("zz-other-%04d", i)
			fact := ProjectNoteOpenFact{ID: id, ProjectID: p.ID, ActorID: 102, OpenedAt: s.Now()}
			if err := r.Insert(projectNoteRecord(projectNoteOpenKind, id, p.Owner, s.Now(), fact, "u:102", "", "", "")); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.MyVisitors(ctx, scope, scope.Actor, Filter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitorItems(t, list)
	if len(items) != 1 || items[0].Key != "u:101" {
		t.Fatalf("coworker traffic displaced own customer: %+v", items)
	}
}

func TestMyCustomerLinkedProjectAppearsWithoutFakingVisit(t *testing.T) {
	s, scope, _ := projectNoteFixture(t)
	ctx := context.Background()
	p, err := s.ProjectNoteCreateProject(ctx, scope, ProjectNoteProjectInput{Name: "已关联未浏览", Owner: "owner-a", CustomerRef: "u:101"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.MyVisitors(ctx, scope, scope.Actor, Filter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitorItems(t, list)
	if len(items) != 1 || items[0].Key != "u:101" || items[0].Views != 0 {
		t.Fatalf("linked customer missing or counted as visit: %+v", items)
	}
	detail, err := s.MyVisitorEntries(ctx, scope, scope.Actor, Filter{Visitor: "u:101", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.ProjectActivities) != 1 || detail.ProjectActivities[0].Kind != "project_linked" || detail.ProjectActivities[0].ProjectID != p.ID {
		t.Fatalf("link fact not distinguished: %+v", detail.ProjectActivities)
	}
}

func TestProjectDesignerSQLPredicateMatchesMemoryOwnershipBeforeLimit(t *testing.T) {
	sql, args := buildScopedQuery("tenant", projectNoteOpenKind, RecordQuery{ProjectDesigner: 7, Limit: 10}, 10)
	for _, fragment := range []string{"project_owner.tenant_id=r.tenant_id", "project_owner.owner_id=r.owner_id", "project_owner.kind='note_project'", "$.designer_id"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("SQL ownership missing %q: %s", fragment, sql)
		}
	}
	found := false
	for _, value := range args {
		if value == int64(7) {
			found = true
		}
	}
	if !found {
		t.Fatal("designer must be bound as parameter")
	}
}
