package telefonist

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// broadcastInfo broadcasts an informational JSON message to all connected WebSocket clients.
//
// Uses a non-blocking send so it is safe to call from within the hub's own run loop
// (e.g. from command handlers), preventing a deadlock where the hub blocks sending
// to a channel that only it drains.
func broadcastInfo(h *WsHub, msg string) {
	data := []byte(msg)
	select {
	case h.broadcast <- data:
	default:
		// Drop the message rather than deadlock; the broadcast buffer (1024 slots)
		// being full indicates the hub is overwhelmed or has already shut down.
	}
}

// statusJSON builds a JSON object string from key-value pairs.
// Keys and values are properly escaped via json.Marshal.
//
// Usage:
//
//	statusJSON("status", "error", "token", "testfile", "message", "something broke")
//	→ {"status":"error","token":"testfile","message":"something broke"}
func statusJSON(kvPairs ...string) string {
	if len(kvPairs)%2 != 0 {
		kvPairs = append(kvPairs, "")
	}
	m := make(map[string]string, len(kvPairs)/2+1)
	for i := 0; i < len(kvPairs); i += 2 {
		m[kvPairs[i]] = kvPairs[i+1]
	}
	m["time"] = time.Now().Format("2.1.2006 15:04:05.000")
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

var ansiRegex = regexp.MustCompile(`[\x1b\x9b][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func formatEventDetails(m map[string]interface{}) string {
	var sb strings.Builder
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "event" || k == "time" || k == "_details" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		if k == "data" {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(stripANSI(s))
				lines := strings.Split(s, "\n")
				for _, line := range lines {
					trimmed := strings.TrimSpace(line)
					if trimmed == "" {
						continue
					}
					v = lines // use the slice for formatting
					break
				}
			}
		}

		valStr := ""
		switch val := v.(type) {
		case []string:
			valStr = strings.Join(val, "\n        ") // indent data lines
		case string:
			valStr = val
		case []byte:
			valStr = string(val)
		default:
			b, err := json.MarshalIndent(val, "", "  ")
			if err == nil {
				valStr = string(b)
			} else {
				valStr = fmt.Sprintf("%v", val)
			}
		}

		prettyKey := strings.Title(k)
		fmt.Fprintf(&sb, "%s: %s\n", prettyKey, valStr)
	}
	return strings.TrimSpace(sb.String())
}
