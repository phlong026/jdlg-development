package tracking

import (
	"context"
	"net/http"
	"testing"
)

func miniprogramBoundaryFixture(t *testing.T) *Service {
	t.Helper()
	service, _ := fixture(t)
	for _, route := range []string{"pages/index/index", "pages/template/template", "pages/distribution/distribution", "pages/report/landing", "pages/projectnoteview/projectnoteview", "pages/aitools/aitools", "pages/aitooldetail/aitooldetail", "pages/aitoolworkbench/aitoolworkbench", "pages/aigenerate/aigenerate", "pages/aivideo/aivideo", "pages/aicost/aicost", "pages/newminnantool/newminnantool", "pages/allrounddesign/allrounddesign"} {
		service.Config.Routes[route] = Route{Title: route, Public: true}
	}
	return service
}

func TestEnterMiniprogramRestrictsOnlyAnonymousRoutes(t *testing.T) {
	service := miniprogramBoundaryFixture(t)
	input := func(route, client string) EntryInput {
		return EntryInput{Route: route, Client: client, Anonymous: "anonymous-boundary", Consent: "v1"}
	}
	publicRoutes := []string{"pages/index/index", "pages/template/template", "pages/distribution/distribution"}
	publicClients := []string{"client-public-0001", "client-public-0002", "client-public-0003"}
	for index, route := range publicRoutes {
		entry, err := service.EnterMiniprogram(context.Background(), input(route, publicClients[index]), 0)
		if err != nil || entry.Account != 0 || entry.Route != route || entry.Token == "" {
			t.Fatalf("anonymous public entry route=%s entry=%+v err=%v", route, entry, err)
		}
	}
	if _, err := service.EnterMiniprogram(context.Background(), input("pages/report/landing", "client-protected-anon"), 0); trackingErrorStatus(err) != http.StatusUnauthorized {
		t.Fatalf("anonymous protected entry status=%d err=%v", trackingErrorStatus(err), err)
	}
	if _, err := service.EnterMiniprogram(context.Background(), input("pages/report/landing", "client-protected-incomplete"), 42, MiniprogramProfileAccess{}); trackingErrorStatus(err) != http.StatusPreconditionRequired {
		t.Fatalf("incomplete authenticated entry status=%d err=%v", trackingErrorStatus(err), err)
	}
	if _, err := service.EnterMiniprogram(context.Background(), input("pages/projectnoteview/projectnoteview", "client-note-missing-avatar"), 42, MiniprogramProfileAccess{}); trackingErrorStatus(err) != http.StatusPreconditionRequired {
		t.Fatalf("note missing avatar status=%d err=%v", trackingErrorStatus(err), err)
	}
	if entry, err := service.EnterMiniprogram(context.Background(), input("pages/report/landing", "client-protected-user"), 42, MiniprogramProfileAccess{Complete: true}); err != nil || entry.Account != 42 {
		t.Fatalf("authenticated entry=%+v err=%v", entry, err)
	}
}

func TestAcceptMiniprogramKeepsAnonymousTokenBoundToPublicAnonymousEntry(t *testing.T) {
	service := miniprogramBoundaryFixture(t)
	public, err := service.EnterMiniprogram(context.Background(), EntryInput{Route: "pages/index/index", Client: "client-public-events", Consent: "v1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	event := ev(service, public, "anonymous-public-event", "page_view", service.Now())
	results, err := service.AcceptMiniprogram(context.Background(), public.Token, []Event{event}, 0)
	if err != nil || len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("anonymous results=%+v err=%v", results, err)
	}
	loggedPublic, err := service.EnterMiniprogram(context.Background(), EntryInput{Route: "pages/index/index", Client: "client-account-public-events", Consent: "v1"}, 42, MiniprogramProfileAccess{})
	if err != nil {
		t.Fatal(err)
	}
	publicAccountEvent := ev(service, loggedPublic, "incomplete-account-public-event", "page_view", service.Now())
	publicAccountEvent.View = "pageview-account-public"
	if results, err = service.AcceptMiniprogram(context.Background(), loggedPublic.Token, []Event{publicAccountEvent}, 42, MiniprogramProfileAccess{}); err != nil || len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("incomplete account public results=%+v err=%v", results, err)
	}

	logged, err := service.EnterMiniprogram(context.Background(), EntryInput{Route: "pages/report/landing", Client: "client-account-events", Consent: "v1"}, 42, MiniprogramProfileAccess{Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	loggedEvent := ev(service, logged, "anonymous-cannot-use-account-event", "page_view", service.Now())
	loggedEvent.View = "pageview-account-bound"
	if _, err = service.AcceptMiniprogram(context.Background(), logged.Token, []Event{loggedEvent}, 0); trackingErrorStatus(err) != http.StatusForbidden {
		t.Fatalf("anonymous account token status=%d err=%v", trackingErrorStatus(err), err)
	}
	if _, err = service.AcceptMiniprogram(context.Background(), logged.Token, []Event{loggedEvent}, 42, MiniprogramProfileAccess{}); trackingErrorStatus(err) != http.StatusPreconditionRequired {
		t.Fatalf("incomplete account event status=%d err=%v", trackingErrorStatus(err), err)
	}
	if results, err = service.AcceptMiniprogram(context.Background(), logged.Token, []Event{loggedEvent}, 42, MiniprogramProfileAccess{Complete: true}); err != nil || len(results) != 1 || results[0].Status != "accepted" {
		t.Fatalf("account results=%+v err=%v", results, err)
	}

	legacyAnonymous, err := service.Enter(context.Background(), EntryInput{Route: "pages/report/landing", Client: "client-old-protected-anon", Consent: "v1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	legacyEvent := ev(service, legacyAnonymous, "old-protected-anonymous-event", "page_view", service.Now())
	legacyEvent.View = "pageview-old-protected-anon"
	if _, err = service.AcceptMiniprogram(context.Background(), legacyAnonymous.Token, []Event{legacyEvent}, 0); trackingErrorStatus(err) != http.StatusForbidden {
		t.Fatalf("protected anonymous token status=%d err=%v", trackingErrorStatus(err), err)
	}
}

func TestCreateMiniprogramShareRestrictsOnlyAnonymousRoutes(t *testing.T) {
	service := miniprogramBoundaryFixture(t)
	public, err := service.EnterMiniprogram(context.Background(), EntryInput{Route: "pages/index/index", Client: "client-public-share", Consent: "v1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateMiniprogramShare(context.Background(), ShareInput{Route: public.Route, Key: "anonymous-public-share"}, public.Token, 0); err != nil {
		t.Fatalf("anonymous public share: %v", err)
	}

	protected, err := service.EnterMiniprogram(context.Background(), EntryInput{Route: "pages/report/landing", Client: "client-account-share", Consent: "v1"}, 42, MiniprogramProfileAccess{Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateMiniprogramShare(context.Background(), ShareInput{Route: protected.Route, Key: "anonymous-protected-share"}, protected.Token, 0); trackingErrorStatus(err) != http.StatusUnauthorized {
		t.Fatalf("anonymous protected share status=%d err=%v", trackingErrorStatus(err), err)
	}
	if _, err = service.CreateMiniprogramShare(context.Background(), ShareInput{Route: protected.Route, Key: "incomplete-account-share"}, protected.Token, 42, MiniprogramProfileAccess{}); trackingErrorStatus(err) != http.StatusPreconditionRequired {
		t.Fatalf("incomplete protected share status=%d err=%v", trackingErrorStatus(err), err)
	}
	if _, err = service.CreateMiniprogramShare(context.Background(), ShareInput{Route: protected.Route, Key: "account-protected-share"}, protected.Token, 42, MiniprogramProfileAccess{Complete: true}); err != nil {
		t.Fatalf("authenticated protected share: %v", err)
	}

	loggedPublic, err := service.EnterMiniprogram(context.Background(), EntryInput{Route: "pages/index/index", Client: "client-account-public-share", Consent: "v1"}, 42, MiniprogramProfileAccess{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateMiniprogramShare(context.Background(), ShareInput{Route: loggedPublic.Route, Key: "incomplete-account-public-share"}, loggedPublic.Token, 42, MiniprogramProfileAccess{}); err != nil {
		t.Fatalf("incomplete account public share: %v", err)
	}
	if _, err = service.CreateMiniprogramShare(context.Background(), ShareInput{Route: loggedPublic.Route, Key: "anonymous-account-token-share"}, loggedPublic.Token, 0); trackingErrorStatus(err) != http.StatusForbidden {
		t.Fatalf("anonymous account token share status=%d err=%v", trackingErrorStatus(err), err)
	}
}

// AI form visits remain observable without converting personal input into events.
// This catches a new form accidentally inheriting the complete-profile gate.
func TestMiniprogramAIFormsTrackBeforeAndAfterAccountLogin(t *testing.T) {
	for _, route := range []string{"pages/aitools/aitools", "pages/aitooldetail/aitooldetail", "pages/aitoolworkbench/aitoolworkbench", "pages/aigenerate/aigenerate", "pages/aivideo/aivideo", "pages/aicost/aicost", "pages/newminnantool/newminnantool", "pages/allrounddesign/allrounddesign"} {
		t.Run(route, func(t *testing.T) {
			svc := miniprogramBoundaryFixture(t)
			content := ""
			if route == "pages/aitooldetail/aitooldetail" || route == "pages/aitoolworkbench/aitoolworkbench" {
				content = LegacyID(route, 7)
				svc.ResolveLegacy = func(actualRoute string, id int64) (LegacyMetadata, error) {
					if actualRoute != route || id != 7 {
						return LegacyMetadata{}, fail(404, "tool unavailable")
					}
					return LegacyMetadata{Title: "公开生图工具", Type: "tool", VersionSeed: "published-tool-7"}, nil
				}
			}
			anon, err := svc.EnterMiniprogram(context.Background(), EntryInput{Route: route, Content: content, Client: "ai-preview-client", Anonymous: "ai-preview-anonymous", Consent: "v1"}, 0)
			if err != nil || anon.Account != 0 || anon.Token == "" {
				t.Fatalf("anonymous AI preview entry=%+v err=%v", anon, err)
			}
			event := ev(svc, anon, "ai-preview-page-event", "page_view", svc.Now())
			results, err := svc.AcceptMiniprogram(context.Background(), anon.Token, []Event{event}, 0)
			if err != nil || len(results) != 1 || results[0].Status != "accepted" {
				t.Fatalf("AI preview events=%+v err=%v", results, err)
			}
			account, err := svc.EnterMiniprogram(context.Background(), EntryInput{Route: route, Content: content, Client: "ai-preview-client", Anonymous: "ai-preview-anonymous", Consent: "v1", Previous: anon.Token}, 42, MiniprogramProfileAccess{})
			if err != nil || account.Account != 42 || account.Session != anon.Session || account.OriginalVisitor != "a:ai-preview-anonymous" {
				t.Fatalf("AI account without avatar/nickname entry=%+v err=%v", account, err)
			}
			accountEvent := ev(svc, account, "ai-account-page-event", "page_view", svc.Now())
			accountEvent.View = "ai-account-page-view"
			results, err = svc.AcceptMiniprogram(context.Background(), account.Token, []Event{accountEvent}, 42, MiniprogramProfileAccess{})
			if err != nil || len(results) != 1 || results[0].Status != "accepted" {
				t.Fatalf("AI account events=%+v err=%v", results, err)
			}
			if _, err = svc.AcceptMiniprogram(context.Background(), account.Token, []Event{accountEvent}, 0); trackingErrorStatus(err) != http.StatusForbidden {
				t.Fatalf("account credential accepted anonymously: %v", err)
			}
		})
	}
}
