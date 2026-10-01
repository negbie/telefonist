package telefonist

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

// Exercise the hub boundary: metadata must remain useful in evidence without
// altering the historical bytes used by existing _hash baselines.
func TestHubPreservesLegacyHashBeforeEnrichment(t *testing.T) {
	const legacyBytes = `{"_agent":"alice",,"event":true,,,"type":"CALL_ESTABLISHED"}` + "\n\n"
	const want = "0f03b168a55a808f"
	for _, runID := range []int64{1, 987654} {
		s := newTrainSession(nil, nil)
		s.runID = runID
		s.started = time.Now().Add(-time.Duration(runID) * time.Second)
		s.eventSequence = uint64(runID)
		h := &WsHub{trainSession: s, history: NewRingBuffer(32)}
		raw := []byte(`{"_agent":"alice","contacturi":"sip:alice@127.0.0.1:1234","event":true,"id":"dynamic","param":"varies","type":"CALL_ESTABLISHED"}`)
		h.handleAgentMsg(AgentMsg{Alias: "alice", Msg: gobaresip.Msg{Event: &gobaresip.EventMsg{Type: "CALL_ESTABLISHED", RawJSON: raw}}})
		// Responses and telemetry newly retained as evidence were never hashed.
		h.handleAgentMsg(AgentMsg{Alias: "alice", Msg: gobaresip.Msg{Response: &gobaresip.ResponseMsg{Data: "ok"}}})
		for _, kind := range []string{"CALL_RTCP", "MODULE", "END_OF_FILE", "NATIVE_DONE", "LOG", "SIP"} {
			noise := []byte(`{"event":true,"type":"` + kind + `","param":"variable"}`)
			h.handleAgentMsg(AgentMsg{Alias: "alice", Msg: gobaresip.Msg{Event: &gobaresip.EventMsg{Type: kind, RawJSON: noise}}})
		}
		if got := s.outputBuf.String(); got != legacyBytes {
			t.Fatalf("legacy hash input changed: %q", got)
		}
		if got := s.finish(); got != want {
			t.Fatalf("legacy hash changed: got %s, want %s", got, want)
		}
		var evidence map[string]any
		if err := json.Unmarshal([]byte(strings.Split(s.GetFullOutput(), "\n\n")[0]), &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence["run_id"] != float64(runID) || evidence["timestamp"] == nil || evidence["elapsed_ms"] == nil || evidence["event_sequence"] != float64(runID+1) {
			t.Fatalf("missing diagnostic metadata: %v", evidence)
		}
	}
}

func TestHashOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"simple string", "hello world"},
		{"json-like", `{"type":"CALL_ESTABLISHED"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hashOutput(tt.input)

			// xxhash64 hex digest is always 16 characters
			if len(hash) != 16 {
				t.Errorf("hashOutput(%q) length = %d, want 16", tt.input, len(hash))
			}

			// Should be deterministic
			hash2 := hashOutput(tt.input)
			if hash != hash2 {
				t.Errorf("hashOutput is not deterministic: %q != %q", hash, hash2)
			}
		})
	}
}

func TestHashOutputDeterministic(t *testing.T) {
	input := `{"event":true,"type":"CALL_ESTABLISHED","peeruri":"sip:foo@bar"}`
	hash1 := hashOutput(input)
	hash2 := hashOutput(input)
	hash3 := hashOutput(input)

	if hash1 != hash2 || hash2 != hash3 {
		t.Errorf("hashOutput should be deterministic: %q, %q, %q", hash1, hash2, hash3)
	}
}

func TestHashOutputDifferentInputs(t *testing.T) {
	hash1 := hashOutput("hello")
	hash2 := hashOutput("world")

	if hash1 == hash2 {
		t.Errorf("different inputs should produce different hashes: %q == %q", hash1, hash2)
	}
}

func TestNewTrainSession(t *testing.T) {
	session := newTrainSession(nil, nil)

	if !session.active {
		t.Error("new session should be active")
	}
	if session.outputBuf.Len() != 0 {
		t.Errorf("new session output buffer should be empty, got %d bytes", session.outputBuf.Len())
	}
}

func TestTrainSessionFinishEmpty(t *testing.T) {
	session := newTrainSession(nil, nil)

	hash := session.finish()

	if len(hash) != 16 {
		t.Errorf("hash length = %d, want 16", len(hash))
	}
}

// Tests call recordHashEvent directly — this mirrors the production path
// in telemetry.go where recordHashEvent is the entry point for hashing.
func TestTrainSessionRecordHashEvent(t *testing.T) {
	session := newTrainSession(nil, nil)

	event := gobaresip.EventMsg{
		Type:    "CALL_ESTABLISHED",
		RawJSON: []byte(`{"event":true,"type":"CALL_ESTABLISHED","id":"123","contacturi":"sip:alice@127.0.0.1:5060","param":"active"}`),
	}

	session.recordHashEvent(event)

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"CALL_ESTABLISHED",,,}` + "\n\n"

	if output != expected {
		t.Errorf("recordHashEvent parsed incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordHashEventCustom(t *testing.T) {
	session := newTrainSession(nil, nil)

	event := gobaresip.EventMsg{
		Type:    "CUSTOM",
		RawJSON: []byte(`{"type":"CUSTOM","id":"ad46907a900c9882_xfer-1_pbx-1","foo":"bar"}`),
	}

	session.recordHashEvent(event)

	output := session.outputBuf.String()
	expected := `{"type":"CUSTOM",,"foo":"bar"}` + "\n\n"

	if output != expected {
		t.Errorf("recordHashEvent parsed incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordHashEventInactive(t *testing.T) {
	session := newTrainSession(nil, nil)
	session.active = false

	session.recordHashEvent(gobaresip.EventMsg{Type: "CALL_ESTABLISHED"})

	output := session.outputBuf.String()
	if output != "" {
		t.Errorf("inactive session should not record event, got %q", output)
	}
}

// CALL_RTCP, MODULE, NATIVE_DONE filtering is now in telemetry.go (the
// production dispatcher), so we test it via handleAgentMsg in
// TestHubPreservesLegacyHashBeforeEnrichment above.

func TestTrainSessionRecordHashEventIgnored(t *testing.T) {
	session := newTrainSession([]string{"CALL_RESUME", "CALL_Hold"}, nil)

	// Should record CALL_ESTABLISHED
	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "CALL_ESTABLISHED",
		RawJSON: []byte(`{"event":true,"type":"CALL_ESTABLISHED","id":"123","contacturi":"sip:alice@127.0.0.1:5060","param":"active"}`),
	})

	// Should NOT record CALL_RESUME
	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "CALL_RESUME",
		RawJSON: []byte(`{"event":true,"type":"CALL_RESUME","id":"456","contacturi":"sip:alice@127.0.0.1:5060","param":"active"}`),
	})

	// Should NOT record CALL_Hold (case insensitive ideally, but we passed exactly that)
	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "cAlL_hOlD",
		RawJSON: []byte(`{"event":true,"type":"cAlL_hOlD","id":"789","contacturi":"sip:alice@127.0.0.1:5060","param":"active"}`),
	})

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"CALL_ESTABLISHED",,,}` + "\n\n"

	if output != expected {
		t.Errorf("Ignored events were recorded incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordHashEventAccepted(t *testing.T) {
	session := newTrainSession(nil, []string{"CALL_ESTABLISHED", "CALL_HOLD"})

	// Should record CALL_ESTABLISHED
	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "CALL_ESTABLISHED",
		RawJSON: []byte(`{"event":true,"type":"CALL_ESTABLISHED","id":"123","contacturi":"sip:alice@127.0.0.1:5060","param":"active"}`),
	})

	// Should NOT record CALL_MENC
	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "CALL_MENC",
		RawJSON: []byte(`{"event":true,"type":"CALL_MENC","id":"456","contacturi":"sip:alice@127.0.0.1:5060","param":"active"}`),
	})

	// Should record CALL_HOLD (case insensitive)
	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "call_hold",
		RawJSON: []byte(`{"event":true,"type":"call_hold","id":"789","contacturi":"sip:alice@127.0.0.1:5060","param":"active"}`),
	})

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"CALL_ESTABLISHED",,,}` + "\n\n" +
		`{"event":true,"type":"call_hold",,,}` + "\n\n"

	if output != expected {
		t.Errorf("Accepted events filtering was incorrect.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordHashEventRegisterFail(t *testing.T) {
	session := newTrainSession(nil, nil)

	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "REGISTER_FAIL",
		RawJSON: []byte(`{"event":true,"type":"REGISTER_FAIL","id":"123"}`),
	})

	if session.failMsg != "Registration failed" {
		t.Errorf("expected failMsg 'Registration failed', got %q", session.failMsg)
	}
}

func TestTrainSessionRecordHashEventExcludedTypes(t *testing.T) {
	session := newTrainSession(nil, nil)

	// These types are excluded from the hash by recordHashEvent.
	// CALL_RTCP, MODULE, NATIVE_DONE are filtered earlier in telemetry.go
	// and never reach recordHashEvent in production.
	excludedTypes := []string{"SHUTDOWN", "UNREGISTERING", "EXIT", "SIP", "LOG"}
	for _, et := range excludedTypes {
		session.recordEvidence([]byte(`{"event":true,"type":"` + et + `","id":"123"}`))
		session.recordHashEvent(gobaresip.EventMsg{
			Type:    et,
			RawJSON: []byte(`{"event":true,"type":"` + et + `","id":"123"}`),
		})
	}

	// Output buffer used for hashing must be empty
	if session.outputBuf.Len() != 0 {
		t.Errorf("expected empty outputBuf for excluded types, got %q", session.outputBuf.String())
	}

	// Full buffer for download/storage must still contain all events
	full := session.GetFullOutput()
	for _, et := range excludedTypes {
		if !strings.Contains(full, `"`+et+`"`) {
			t.Errorf("full output should contain %s, got: %s", et, full)
		}
	}
}

func TestTrainSessionRecordHashEventContacturiAndParam(t *testing.T) {
	session := newTrainSession(nil, nil)

	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "CALL_CLOSED",
		RawJSON: []byte(`{"event":true,"type":"CALL_CLOSED","id":"abc","contacturi":"sip:alice@127.0.0.1:5060","param":"Connection reset"}`),
	})

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"CALL_CLOSED",,,}` + "\n\n"

	if output != expected {
		t.Errorf("recordHashEvent parsed incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordHashEventAudioReportAndPresence(t *testing.T) {
	session := newTrainSession(nil, nil)

	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "AUDIO_REPORT",
		RawJSON: []byte(`{"event":true,"type":"AUDIO_REPORT","id":"123","contacturi":"sip:alice@127.0.0.1","param":"peeruri=sip:bob@biloxi.com;tx=true;"}`),
	})
	session.recordHashEvent(gobaresip.EventMsg{
		Type:    "PRESENCE",
		RawJSON: []byte(`{"event":true,"type":"PRESENCE","id":"456","param":"firststate=open;"}`),
	})

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"AUDIO_REPORT",,,` + `"param":"peeruri=sip:bob@biloxi.com;tx=true;"}` + "\n\n" +
		`{"event":true,"type":"PRESENCE",,"param":"firststate=open;"}` + "\n\n"

	if output != expected {
		t.Errorf("recordHashEvent for AUDIO_REPORT and PRESENCE parsed incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionFinishDeterministicHash(t *testing.T) {
	// Two runs with different dynamic contacturi, param, id, plus lifecycle events should produce identical hash
	s1 := newTrainSession(nil, nil)
	s1.recordHashEvent(gobaresip.EventMsg{
		Type:    "CALL_CLOSED",
		RawJSON: []byte(`{"event":true,"type":"CALL_CLOSED","id":"id1","contacturi":"sip:alice@10.0.0.1:1234","param":"normal"}`),
	})
	s1.recordHashEvent(gobaresip.EventMsg{
		Type:    "SHUTDOWN",
		RawJSON: []byte(`{"event":true,"type":"SHUTDOWN","id":"id2"}`),
	})

	s2 := newTrainSession(nil, nil)
	s2.recordHashEvent(gobaresip.EventMsg{
		Type:    "CALL_CLOSED",
		RawJSON: []byte(`{"event":true,"type":"CALL_CLOSED","id":"id3","contacturi":"sip:alice@10.0.0.2:5678","param":"other"}`),
	})
	s2.recordHashEvent(gobaresip.EventMsg{
		Type:    "UNREGISTERING",
		RawJSON: []byte(`{"event":true,"type":"UNREGISTERING","id":"id4"}`),
	})
	s2.recordHashEvent(gobaresip.EventMsg{
		Type:    "EXIT",
		RawJSON: []byte(`{"event":true,"type":"EXIT","id":"id5"}`),
	})

	h1 := s1.finish()
	h2 := s2.finish()
	if h1 != h2 {
		t.Errorf("hashes should match despite dynamic fields and lifecycle events: %q != %q", h1, h2)
	}
}

func TestTrainSessionFinishDifferentiatesAudioReportAndPresence(t *testing.T) {
	// Different param in AUDIO_REPORT must produce different hashes
	sAR1 := newTrainSession(nil, nil)
	sAR1.recordHashEvent(gobaresip.EventMsg{Type: "AUDIO_REPORT", RawJSON: []byte(`{"event":true,"type":"AUDIO_REPORT","param":"tx=true;"}`)})
	sAR2 := newTrainSession(nil, nil)
	sAR2.recordHashEvent(gobaresip.EventMsg{Type: "AUDIO_REPORT", RawJSON: []byte(`{"event":true,"type":"AUDIO_REPORT","param":"tx=false;"}`)})

	if sAR1.finish() == sAR2.finish() {
		t.Errorf("AUDIO_REPORT with different param should produce different hashes")
	}

	// Different param in PRESENCE must produce different hashes
	sPres1 := newTrainSession(nil, nil)
	sPres1.recordHashEvent(gobaresip.EventMsg{Type: "PRESENCE", RawJSON: []byte(`{"event":true,"type":"PRESENCE","param":"firststate=open;"}`)})
	sPres2 := newTrainSession(nil, nil)
	sPres2.recordHashEvent(gobaresip.EventMsg{Type: "PRESENCE", RawJSON: []byte(`{"event":true,"type":"PRESENCE","param":"firststate=closed;"}`)})

	if sPres1.finish() == sPres2.finish() {
		t.Errorf("PRESENCE with different param should produce different hashes")
	}
}
