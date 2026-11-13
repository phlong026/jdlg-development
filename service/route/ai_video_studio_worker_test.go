package route

import (
	"context"
	"encoding/json"
	"service/aivideostudio"
	"service/model"
	"testing"
)

func TestStudioPersistedDraftBuildMeetsProviderContract(t *testing.T) {
	for _, resolution := range []string{"768p", "2k"} {
		r, e := buildStudioGeneration(context.Background(), nil, model.StudioWorkerTask{ID: "task", Snapshot: model.AIVideoStudioDraft{Mode: "text", Prompt: "courtyard", Ratio: "16:9", Resolution: resolution, Duration: 4, References: json.RawMessage(`[]`)}})
		if e != nil {
			t.Fatal(e)
		}
		if e = aivideostudio.ValidateGenerationRequest(r); e != nil {
			t.Fatal("stored draft violates provider contract", resolution, e)
		}
	}
}
