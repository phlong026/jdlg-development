package route

import "testing"

func TestBrowseTemplateMappingRequiresExplicitVerifiedFields(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"thing1":"guessed"}`, `{"bad.field":"title"}`, `{"thing1":"title","thing1":"body"}`, `{"thing1":"title"} {}`} {
		if _, err := parseNoticeTemplateFields(raw); err == nil {
			t.Fatalf("accepted unsafe/missing mapping %q", raw)
		}
	}
	fields, err := parseNoticeTemplateFields(`{"thing1":"title","time2":"time"}`)
	if err != nil || fields["thing1"] != "title" {
		t.Fatalf("valid mapping %v %v", fields, err)
	}
}
