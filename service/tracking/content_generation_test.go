package tracking

import (
	"context"
	"testing"
)

func TestGenerationDraftRetryIsIdempotentWithoutMergingAnExplicitCopy(t *testing.T) {
	svc, scope := fixture(t)
	ctx := context.Background()
	in := Draft{Title: "生成结果", Body: "原始结果", BodyFormat: "plain", Owner: scope.Owners[0], Rights: "own"}
	first, err := svc.SaveContentFromGeneration(ctx, scope, "task-0000001", "save-operation-0001", in)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := svc.SaveContentFromGeneration(ctx, scope, "task-0000001", "save-operation-0001", in)
	if err != nil || retry.ID != first.ID {
		t.Fatalf("响应丢失重试必须返回同一草稿: first=%s retry=%s err=%v", first.ID, retry.ID, err)
	}
	edit := in
	edit.Body = "用户已修改的正文"
	edit.Revision = first.Revision
	edited, err := svc.SaveContent(ctx, scope, first.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = svc.SaveContentFromGeneration(ctx, scope, "task-0000001", "save-operation-0001", in)
	if err != nil || retry.Body != edited.Body || retry.Revision != edited.Revision {
		t.Fatalf("重试不得覆盖已编辑草稿: %+v %v", retry, err)
	}
	copy, err := svc.SaveContentFromGeneration(ctx, scope, "task-0000001", "save-operation-0002", in)
	if err != nil || copy.ID == first.ID {
		t.Fatalf("明确另存需要新的草稿: %+v %v", copy, err)
	}
	conflict := in
	conflict.Title = "不同参数"
	if _, err = svc.SaveContentFromGeneration(ctx, scope, "task-0000001", "save-operation-0001", conflict); err == nil {
		t.Fatal("同键不同参数不得覆盖")
	}
	if _, err = svc.DeleteContentDraft(ctx, scope, first.ID, edited.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SaveContentFromGeneration(ctx, scope, "task-0000001", "save-operation-0001", in); err == nil {
		t.Fatal("已删除草稿不得靠重放复活")
	}
}
