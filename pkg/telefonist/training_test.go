package telefonist

import (
	"testing"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

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

func TestTrainSessionRecordEvent(t *testing.T) {
	session := newTrainSession(nil, nil)

	event := gobaresip.EventMsg{
		Type:    "CALL_ESTABLISHED",
		RawJSON: []byte(`{"event":true,"type":"CALL_ESTABLISHED","id":"123"}`),
	}

	session.recordEvent(event)

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"CALL_ESTABLISHED","id":""}` + "\n\n"

	if output != expected {
		t.Errorf("recordEvent parsed incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordEventCustom(t *testing.T) {
	session := newTrainSession(nil, nil)

	event := gobaresip.EventMsg{
		Type:    "CUSTOM",
		RawJSON: []byte(`{"type":"CUSTOM","id":"ad46907a900c9882_xfer-1_pbx-1","foo":"bar"}`),
	}

	session.recordEvent(event)

	output := session.outputBuf.String()
	expected := `{"type":"CUSTOM","id":"","foo":"bar"}` + "\n\n"

	if output != expected {
		t.Errorf("recordEvent parsed incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordEventInactive(t *testing.T) {
	session := newTrainSession(nil, nil)
	session.active = false

	session.recordEvent(gobaresip.EventMsg{Type: "CALL_ESTABLISHED"})

	output := session.outputBuf.String()
	if output != "" {
		t.Errorf("inactive session should not record event, got %q", output)
	}
}

func TestTrainSessionRecordEventRTCP(t *testing.T) {
	session := newTrainSession(nil, nil)
	session.recordEvent(gobaresip.EventMsg{Type: "CALL_RTCP"})

	output := session.outputBuf.String()
	if output != "" {
		t.Errorf("RTCP event should not be recorded, got %q", output)
	}
}

func TestTrainSessionRecordEventIgnored(t *testing.T) {
	session := newTrainSession([]string{"CALL_RESUME", "CALL_Hold"}, nil)

	// Should record CALL_ESTABLISHED
	session.recordEvent(gobaresip.EventMsg{
		Type:    "CALL_ESTABLISHED",
		RawJSON: []byte(`{"event":true,"type":"CALL_ESTABLISHED","id":"123"}`),
	})

	// Should NOT record CALL_RESUME
	session.recordEvent(gobaresip.EventMsg{
		Type:    "CALL_RESUME",
		RawJSON: []byte(`{"event":true,"type":"CALL_RESUME","id":"456"}`),
	})

	// Should NOT record CALL_Hold (case insensitive ideally, but we passed exactly that)
	session.recordEvent(gobaresip.EventMsg{
		Type:    "cAlL_hOlD",
		RawJSON: []byte(`{"event":true,"type":"cAlL_hOlD","id":"789"}`),
	})

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"CALL_ESTABLISHED","id":""}` + "\n\n"

	if output != expected {
		t.Errorf("Ignored events were recorded incorrectly.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordEventAccepted(t *testing.T) {
	session := newTrainSession(nil, []string{"CALL_ESTABLISHED", "CALL_HOLD"})

	// Should record CALL_ESTABLISHED
	session.recordEvent(gobaresip.EventMsg{
		Type:    "CALL_ESTABLISHED",
		RawJSON: []byte(`{"event":true,"type":"CALL_ESTABLISHED","id":"123"}`),
	})

	// Should NOT record CALL_MENC
	session.recordEvent(gobaresip.EventMsg{
		Type:    "CALL_MENC",
		RawJSON: []byte(`{"event":true,"type":"CALL_MENC","id":"456"}`),
	})

	// Should record CALL_HOLD (case insensitive)
	session.recordEvent(gobaresip.EventMsg{
		Type:    "call_hold",
		RawJSON: []byte(`{"event":true,"type":"call_hold","id":"789"}`),
	})

	output := session.outputBuf.String()
	expected := `{"event":true,"type":"CALL_ESTABLISHED","id":""}` + "\n\n" +
		`{"event":true,"type":"call_hold","id":""}` + "\n\n"

	if output != expected {
		t.Errorf("Accepted events filtering was incorrect.\nExpected:\n%q\nGot:\n%q", expected, output)
	}
}

func TestTrainSessionRecordEventRegisterFail(t *testing.T) {
	session := newTrainSession(nil, nil)

	session.recordEvent(gobaresip.EventMsg{
		Type:    "REGISTER_FAIL",
		RawJSON: []byte(`{"event":true,"type":"REGISTER_FAIL","id":"123"}`),
	})

	if session.failMsg != "Registration failed" {
		t.Errorf("expected failMsg 'Registration failed', got %q", session.failMsg)
	}
}
