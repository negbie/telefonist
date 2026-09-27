package telefonist

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var sanitizeRegexp = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// SanitizeName cleans up project and testfile names securely.
func SanitizeName(name string) string {
	if name == "" {
		return ""
	}
	return sanitizeRegexp.ReplaceAllString(name, "_")
}

// DefaultTimeFormat is the standard format used by the Telefonist UI.
const DefaultTimeFormat = "2.1.2006 15:04:05.000"

// FormatNow returns the current local time in the default format.
func FormatNow() string {
	return time.Now().Format(DefaultTimeFormat)
}

// EnrichMessage injects standard metadata (agent, time, type) into raw JSON messages.
func EnrichMessage(raw []byte, agent, msgType string) ([]byte, error) {
	m := make(map[string]interface{})
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
	}
	m["_agent"] = agent
	m["time"] = FormatNow()
	if msgType != "" {
		m["type"] = msgType
		m["event"] = true
	}
	return json.Marshal(m)
}

// ExtractAlias pulls the local alias part out of a SIP AoR or URI string.
func ExtractAlias(s string) string {
	// 1. Extract content between < > if present
	start := strings.Index(s, "<")
	end := strings.Index(s, ">")
	if start != -1 && end != -1 && end > start {
		s = s[start+1 : end]
	}

	// 2. Strip everything after ; as per your requirement
	if idx := strings.Index(s, ";"); idx != -1 {
		s = s[:idx]
	}

	return s
}

var authPassRegexp = regexp.MustCompile(`(?i)(auth_pass=)([^; \r\n\t"\\]*)`)
var authPassKeywordRegexp = regexp.MustCompile(`(?i)auth_pass`)

// HasAuthPass checks case-insensitively whether s contains "auth_pass" without regex allocations.
func HasAuthPass(s string) bool {
	const target = "auth_pass"
	if len(s) < len(target) {
		return false
	}
	for i := 0; i <= len(s)-len(target); i++ {
		match := true
		for j := 0; j < len(target); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != target[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// HasAuthPassBytes checks case-insensitively whether b contains "auth_pass" without regex allocations.
func HasAuthPassBytes(b []byte) bool {
	const target = "auth_pass"
	if len(b) < len(target) {
		return false
	}
	for i := 0; i <= len(b)-len(target); i++ {
		match := true
		for j := 0; j < len(target); j++ {
			c := b[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != target[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// SanitizeString masks sensitive values like auth_pass in a string.
func SanitizeString(s string) string {
	if s == "" || !HasAuthPass(s) {
		return s
	}
	return authPassRegexp.ReplaceAllString(s, "${1}******")
}

// SanitizeBytes masks sensitive values like auth_pass in a byte slice.
func SanitizeBytes(b []byte) []byte {
	if len(b) == 0 || !HasAuthPassBytes(b) {
		return b
	}
	return authPassRegexp.ReplaceAll(b, []byte("${1}******"))
}
