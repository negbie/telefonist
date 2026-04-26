package telefonist

import "encoding/json"

func broadcastInfo(h *WsHub, msg string) {
	select {
	case h.broadcast <- []byte(msg):
	default:
	}
}

// statusJSON builds a JSON object string from a key-value map, adding a "time" field.
func statusJSON(kv map[string]string) string {
	if kv == nil {
		kv = make(map[string]string)
	}
	kv["time"] = FormatNow()
	b, err := json.Marshal(kv)
	if err != nil {
		return "{}"
	}
	return string(b)
}
