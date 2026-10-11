package route

import (
	"database/sql"
	"testing"

	"service/model"
)

type fakeOriginalTaskLookup struct {
	task *model.AITask
	err  error
}

func (f fakeOriginalTaskLookup) GetByID(id int64) (*model.AITask, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.task, nil
}

func TestTemplateToResponseOnlyReportsUsableOriginalTask(t *testing.T) {
	template := &model.Template{ID: 1, OriginalTaskID: 99}

	missingTask := templateToResponse(template, false, nil, fakeOriginalTaskLookup{err: sql.ErrNoRows})
	if got := missingTask["has_original_task"]; got != false {
		t.Fatalf("expected missing original task to report false, got %#v", got)
	}

	existingTask := templateToResponse(template, false, nil, fakeOriginalTaskLookup{task: &model.AITask{ID: 99}})
	if got := existingTask["has_original_task"]; got != true {
		t.Fatalf("expected existing original task to report true, got %#v", got)
	}
}
