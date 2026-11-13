package model

import (
	"context"
	"testing"
)

func TestStudioWorkerRequiresDurableStore(t *testing.T) {
	w := StudioWorker{}
	if w.Step(context.Background()) == nil {
		t.Fatal("worker must fail closed without durable store")
	}
}
