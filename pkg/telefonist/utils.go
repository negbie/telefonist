package telefonist

import (
	"encoding/json"
	"strings"
	"time"
)

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
