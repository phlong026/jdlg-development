package route

import "testing"

func TestNewMinnanToolSceneDoesNotRequireAIToolRecord(t *testing.T) {
	if isAIToolPayloadScene("new_minnan_tool_text_to_image") {
		t.Fatal("new_minnan_tool_text_to_image should not be validated through ai_tools records")
	}
	if isAIToolPayloadScene("new_minnan_tool_image_to_image") {
		t.Fatal("new_minnan_tool_image_to_image should not be validated through ai_tools records")
	}
}
