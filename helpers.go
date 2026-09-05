package main

import (
	"encoding/base64"
	"strings"
)

// base64StdEncode encodes bytes to a base64 string.
func base64StdEncode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// base64StdDecode decodes a base64 string to bytes.
func base64StdDecode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// splitAndTrim splits a string by sep and trims each element.
func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// csvEscape quotes a value for CSV output when needed.
func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}
