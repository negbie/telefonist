package telefonist

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// One envelope is used for persistence, scripts, and live display. Time remains
// readable for existing UI; timestamp/elapsed_ms are numeric milliseconds.
func (h *WsHub) handleAgentMsg(am AgentMsg) {
	var value map[string]any
	m := am.Msg
	switch {
	case m.Event != nil:
		if h.onEvent != nil {
			h.onEvent(*m.Event)
		}
		if err := json.Unmarshal(m.Event.RawJSON, &value); err != nil {
			h.failTelemetry("invalid agent event JSON")
			return
		}
	case m.Response != nil:
		if h.onResponse != nil {
			r := *m.Response
			r.Data = SanitizeString(r.Data)
			h.onResponse(r)
		}
		value = map[string]any{"event": true, "type": "RESPONSE", "param": m.Response.Data, "ok": m.Response.Ok, "request_id": m.Response.Token}
	case m.Log != "":
		value = map[string]any{"event": true, "type": "LOG", "param": m.Log}
	case m.SIP != "":
		value = map[string]any{"event": true, "type": "SIP", "param": m.SIP}
	default:
		return
	}
	if value == nil {
		h.failTelemetry("null agent event")
		return
	}
	value["_agent"] = am.Alias
	value["time"] = FormatNow()
	value["timestamp"] = time.Now().UnixMilli()
	value["schema_version"] = 2
	kind, _ := value["type"].(string)
	param, _ := value["param"].(string)
	value["param"] = SanitizeString(param)
	// Request tokens used to contain entire commands, including passwords.
	delete(value, "token")
	if kind == "SIP" {
		addSIPMetadata(value, param)
	}
	if s := h.trainSession; s != nil {
		s.eventSequence++
		value["event_sequence"] = s.eventSequence
		value["run_id"] = s.runID
		value["elapsed_ms"] = time.Since(s.started).Milliseconds()
	}
	raw, err := json.Marshal(value)
	if err != nil {
		h.failTelemetry(err.Error())
		return
	}
	if s := h.trainSession; s != nil {
		s.recordEvidence(raw)
		if m.Event != nil {
			s.recordHashEvent(*m.Event)
		}
		if kind == "TELEMETRY_LOSS" || kind == "AGENT_EXIT" {
			s.failMsg = fmt.Sprintf("%s: %s", kind, param)
		}
		if s.failMsg != "" {
			h.cancelTest()
		}
	}
	// Display filters never change evidence used by assertions.
	if kind == "LOG" && h.ShouldFilterLog(param) {
		return
	}
	h.recordAndBroadcast(raw)
}

func (h *WsHub) failTelemetry(reason string) {
	if h.trainSession != nil {
		h.trainSession.failMsg = "incomplete telemetry: " + reason
		h.cancelTest()
	}
}

func addSIPMetadata(value map[string]any, trace string) {
	lines := strings.Split(trace, "\n")
	if len(lines) > 0 {
		if strings.Contains(lines[0], "|TX") {
			value["direction"] = "OUT"
		} else if strings.Contains(lines[0], "|RX") {
			value["direction"] = "IN"
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == "SIP/2.0" {
			value["method"] = fields[0]
		}
		if len(fields) >= 2 && fields[0] == "SIP/2.0" {
			value["status_code"] = fields[1]
		}
		if strings.HasPrefix(strings.ToLower(line), "call-id:") {
			value["call_id"] = strings.TrimSpace(line[len("call-id:"):])
		}
	}
}
