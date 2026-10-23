package tracking

import (
	"context"
	"testing"
	"time"
)

func TestVisitsIncludePublicPagesByRouteWithoutPrivatePayload(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	s.Config.Routes["pages/index/index"] = Route{Title: "首页", Public: true}
	s.Config.Routes["pages/aicost/aicost"] = Route{Title: "测算入口", Public: true}
	s.Config.Routes["pages/report/questionnaire"] = Route{Title: "私有问卷", Public: false}
	for i, route := range []string{"pages/index/index", "pages/aicost/aicost", "pages/report/questionnaire"} {
		at := now.Add(-time.Hour + time.Duration(i)*time.Minute)
		ev := Event{ID: "public-page-" + route, View: route, Session: "public-pages-session", Route: route, Type: "page_view", Occurred: at, Received: at}
		row := record("event", ev.ID, "one", at, ev)
		row.Visitor, row.Session = "u:1", ev.Session
		if err := m.Put(row); err != nil {
			t.Fatal(err)
		}
	}
	for _, group := range []string{"", "batch", "day"} {
		list, err := s.Visits(context.Background(), scope, Filter{Owner: "one", Group: group})
		if err != nil {
			t.Fatal(err)
		}
		items := visitItems(t, list)
		got := map[string]string{}
		for _, v := range items {
			if len(v.Materials) == 0 {
				got[v.Content.Route] = v.Content.Title
			} else {
				for _, p := range v.Materials {
					got[p.Content.Route] = p.Content.Title
				}
			}
		}
		if len(got) != 2 || got["pages/index/index"] != "首页" || got["pages/aicost/aicost"] != "测算入口" {
			t.Fatalf("%s: %+v", group, got)
		}
	}
	list, err := s.Visits(context.Background(), scope, Filter{Owner: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(visitItems(t, list)) != 0 {
		t.Fatal("public page escaped owner scope")
	}
}
