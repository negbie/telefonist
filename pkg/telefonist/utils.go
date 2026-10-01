package telefonist

import (
	"log"
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

// scanTime parses a time string stored in RFC3339Nano format.
// On parse failure it logs a warning and returns time.Time{}.
func scanTime(raw, label string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		log.Printf("failed to parse %s %q: %v", label, raw, err)
	}
	return t.Local()
}

// NormalizeParamString normalizes a semicolon-delimited SIP parameter string.
// It ensures a leading semicolon, collapses doubles, and trims trailing cruft.
func NormalizeParamString(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	for strings.Contains(p, ";;") {
		p = strings.ReplaceAll(p, ";;", ";")
	}
	if !strings.HasPrefix(p, ";") {
		p = ";" + p
	}
	p = strings.TrimRight(p, "; ")
	return p
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
