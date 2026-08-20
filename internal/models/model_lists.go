package models

import (
	"encoding/json"
	"strings"

	"gorm.io/datatypes"
)

// NormalizeModelList trims model names, removes empty entries, and preserves
// the first occurrence of each exact (case-sensitive) model name.
func NormalizeModelList(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}

	return normalized
}

// EncodeModelList stores a normalized model list as a canonical JSON array.
func EncodeModelList(values []string) datatypes.JSON {
	encoded, err := json.Marshal(NormalizeModelList(values))
	if err != nil {
		return datatypes.JSON("[]")
	}
	return datatypes.JSON(encoded)
}

// DecodeModelList reads a model list from JSON. Historical NULL, empty, and
// malformed values are treated as an empty list.
func DecodeModelList(value datatypes.JSON) []string {
	if len(value) == 0 || string(value) == "null" {
		return []string{}
	}

	var values []string
	if err := json.Unmarshal(value, &values); err != nil {
		return []string{}
	}
	return NormalizeModelList(values)
}
