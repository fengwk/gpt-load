package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestExtractModelOpenAIBody proves the shared extractor handles the standard
// OpenAI JSON request format used before aggregate selection.
func TestExtractModelOpenAIBody(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	got := ExtractModel("openai", c, []byte(`{"model":"gpt-4.1"}`))
	if got != "gpt-4.1" {
		t.Fatalf("ExtractModel() = %q, want %q", got, "gpt-4.1")
	}
}

// TestExtractModelGeminiNativeURL proves native Gemini model names come from
// the URL path even when the body has no model field.
func TestExtractModelGeminiNativeURL(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1beta/models/gemini-2.5-flash:generateContent",
		nil,
	)

	got := ExtractModel("gemini", c, []byte(`{"contents":[]}`))
	if got != "gemini-2.5-flash" {
		t.Fatalf("ExtractModel() = %q, want %q", got, "gemini-2.5-flash")
	}
}

// TestExtractModelReturnsEmptyForInvalidBody proves parse failures become the
// documented no-route-match signal instead of an error.
func TestExtractModelReturnsEmptyForInvalidBody(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	if got := ExtractModel("openai", c, []byte(`not-json`)); got != "" {
		t.Fatalf("ExtractModel() = %q, want an empty model", got)
	}
}
