package telefonist

import "encoding/json"

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
	m := make(map[string]string, len(kvPairs)/2)
	for i := 0; i < len(kvPairs); i += 2 {
		m[kvPairs[i]] = kvPairs[i+1]
	}
	b, _ := json.Marshal(m)
	return string(b)
}

