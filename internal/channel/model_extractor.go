package channel

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
)

// ExtractModel extracts the client-requested model using the channel's request
// format. It is intentionally independent of a ChannelProxy instance so
// aggregate routing can happen before a sub-group channel is selected.
func ExtractModel(channelType string, c *gin.Context, bodyBytes []byte) string {
	if channelType == "gemini" {
		if model := extractGeminiURLModel(c); model != "" {
			return model
		}
	}

	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return ""
	}
	return payload.Model
}

func extractGeminiURLModel(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}

	parts := strings.Split(c.Request.URL.Path, "/")
	for i, part := range parts {
		if part == "models" && i+1 < len(parts) {
			return strings.Split(parts[i+1], ":")[0]
		}
	}
	return ""
}
