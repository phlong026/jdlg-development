package tracking

import (
	"context"
	"testing"
)

func TestImportedDraftIdempotencyDoesNotDuplicateOrOverwrite(t *testing.T) {
	s, scope := fixture(t)
	in := Draft{Title: "导入文章", Body: "正文", Rights: "authorized", Owner: "owner-a", SourceURL: "https://mp.weixin.qq.com/s/article-one"}
	first, err := s.SaveImportedDraft(context.Background(), scope, "import-job-0001", in, map[string]string{"source_gh_id": "gh_example"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.SaveImportedDraft(context.Background(), scope, "import-job-0001", in, map[string]string{"source_gh_id": "gh_example"})
	if err != nil || first.ID != again.ID || again.Revision != 1 {
		t.Fatal(first, again, err)
	}
	in.Title = "不同输入"
	if _, err = s.SaveImportedDraft(context.Background(), scope, "import-job-0001", in, nil); err == nil {
		t.Fatal("idempotency mismatch accepted")
	}
	other := scope
	other.Owners = []string{"other"}
	if _, err = s.SaveImportedDraft(context.Background(), other, "import-job-0001", in, nil); err == nil {
		t.Fatal("cross owner import")
	}
	if first.Status != "draft" || first.Version != 0 {
		t.Fatal("auto-published", first)
	}
}
