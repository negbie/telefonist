package telefonist

import "encoding/json"

func broadcastInfo(h *WsHub, msg string) {
	select {
	case h.broadcast <- []byte(msg):
	default:
	}
}

// statusJSON builds a JSON object string from key-value pairs.
func statusJSON(kvPairs ...string) string {
	if len(kvPairs)%2 != 0 {
		kvPairs = append(kvPairs, "")
	}
	m := make(map[string]string, len(kvPairs)/2+1)
	for i := 0; i < len(kvPairs); i += 2 {
		m[kvPairs[i]] = kvPairs[i+1]
	}
	m["time"] = FormatNow()
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
