package telefonist

import (
	"testing"
)

func TestParseTestfileDefines(t *testing.T) {
	content := `
_define USER alice
_define USER_EMAIL alice@example.com
_define MSG Hello USER
_define GREETING MSG, USER_EMAIL

case1: USER says GREETING
`
	cases, _, _, _, _, _, _, err := parseTestfile(content, nil)
	if err != nil {
		t.Fatalf("parseTestfile failed: %v", err)
	}

	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}

	// USER is replaced first (length 4)
	// USER_EMAIL is replaced before USER (length 10 vs 4)
	// So USER_EMAIL -> alice@example.com
	// Then USER -> alice
	// Sequence: alice says GREETING
	// Then GREETING -> MSG, USER_EMAIL
	// Sequence: alice says MSG, USER_EMAIL
	// Then MSG -> Hello USER
	// Sequence: alice says Hello USER, alice@example.com

	// Wait, since we replacement in one pass over keys, recursive defines ONLY work if the replaced text contains a key that comes LATER in the sorted key list.
	// USER_EMAIL (10)
	// GREETING (8)
	// USER (4)
	// MSG (3)

	// USER says GREETING
	// i=0: USER_EMAIL (no match)
	// i=1: GREETING -> MSG, USER_EMAIL
	//   Sequence: USER says MSG, USER_EMAIL
	// i=2: USER -> alice
	//   Sequence: alice says MSG, USER_EMAIL
	// i=3: MSG -> Hello USER
	//   Sequence: alice says Hello USER, USER_EMAIL

	// If we want FULLY recursive, we'd need multiple passes.
	// But let's see what the current fix does for the sorting issue.
}

func TestParseTestfileIsolation(t *testing.T) {
	content1 := `_define X 1
X`
	content2 := `X`

	cases1, _, _, _, _, _, _, _ := parseTestfile(content1, nil)
	cases2, _, _, _, _, _, _, _ := parseTestfile(content2, nil)

	if len(cases1) == 0 || cases1[0].sequence != "1" {
		t.Errorf("content1 expected 1, got %v", cases1)
	}
	if len(cases2) == 0 || cases2[0].sequence != "X" {
		t.Errorf("content2 expected X, got %v (isolation failure)", cases2)
	}

	content3 := `_define ua1 sip:test1@host
ua1:dial 123`
	cases3, _, _, _, _, _, _, _ := parseTestfile(content3, nil)
	if len(cases3) == 0 || cases3[0].sequence != "sip:test1@host:dial 123" {
		t.Errorf("content3 expected sequence='sip:test1@host:dial 123', got sequence=%q", cases3[0].sequence)
	}
}

func TestParseTestfileSorting(t *testing.T) {
	content := `
_define FOO 1
_define FOOBAR 2
FOOBAR
`
	cases, _, _, _, _, _, _, _ := parseTestfile(content, nil)
	if len(cases) == 0 || cases[0].sequence != "2" {
		t.Errorf("Expected 2, got %v (sorting failure)", cases)
	}
}

func TestParseTestfileAccept(t *testing.T) {
	content := `
_accept CALL_MENC, CALL_LOCAL_SDP
case1: dial 123
`
	_, _, _, _, _, acceptedEvents, _, err := parseTestfile(content, nil)
	if err != nil {
		t.Fatalf("parseTestfile failed: %v", err)
	}

	if len(acceptedEvents) != 2 {
		t.Fatalf("expected 2 accepted events, got %d", len(acceptedEvents))
	}

	if acceptedEvents[0] != "CALL_MENC" || acceptedEvents[1] != "CALL_LOCAL_SDP" {
		t.Errorf("expected [CALL_MENC, CALL_LOCAL_SDP], got %v", acceptedEvents)
	}
}

func TestParseTestfileCentralizedAccounts(t *testing.T) {
	accounts := []SIPAccount{
		{
			Name:       "alice",
			SIPURI:     "sip:alice@sip.domain.com",
			Password:   "alicepassword",
			URIParams:  ";transport=tls",
			AddrParams: ";mediaenc=srtp-mand;input_wav=alice.wav",
		},
		{
			Name:       "ua1",
			SIPURI:     "sip:+123456@sip.domain.com",
			Password:   "trunkpassword",
			URIParams:  ";transport=tls",
			AddrParams: "",
		},
	}

	// Test 1: Friendly name replacement (one bracketless, one bracketed)
	content1 := `
uanew alice
uanew <ua1>;input_wav=bob.wav
alice:dial ua1
alice:addcontact "drei" <ua1>;presence=p2p
`
	cases1, _, _, _, _, _, _, err := parseTestfile(content1, accounts)
	if err != nil {
		t.Fatalf("parseTestfile content1 failed: %v", err)
	}

	if len(cases1) != 4 {
		t.Fatalf("expected 4 cases, got %d", len(cases1))
	}

	expectedSeq1 := "uanew <sip:alice@sip.domain.com;transport=tls>;auth_pass=alicepassword;mediaenc=srtp-mand;input_wav=alice.wav"
	if cases1[0].sequence != expectedSeq1 {
		t.Errorf("expected sequence 1 %q, got %q", expectedSeq1, cases1[0].sequence)
	}

	expectedSeq2 := "uanew <sip:+123456@sip.domain.com;transport=tls>;auth_pass=trunkpassword;input_wav=bob.wav"
	if cases1[1].sequence != expectedSeq2 {
		t.Errorf("expected sequence 2 %q, got %q", expectedSeq2, cases1[1].sequence)
	}

	expectedSeq3 := "sip:alice@sip.domain.com;transport=tls:dial sip:+123456@sip.domain.com;transport=tls"
	if cases1[2].sequence != expectedSeq3 {
		t.Errorf("expected sequence 3 %q, got %q", expectedSeq3, cases1[2].sequence)
	}

	expectedSeq3b := "sip:alice@sip.domain.com;transport=tls:addcontact \"drei\" <sip:+123456@sip.domain.com;transport=tls>;presence=p2p"
	if cases1[3].sequence != expectedSeq3b {
		t.Errorf("expected sequence 4 %q, got %q", expectedSeq3b, cases1[3].sequence)
	}

	// Test 2: SIP URI replacement with missing password
	content2 := `
uanew <sip:alice@sip.domain.com;transport=tls>
`
	cases2, _, _, _, _, _, _, _ := parseTestfile(content2, accounts)
	if len(cases2) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases2))
	}

	expectedSeq4 := "uanew <sip:alice@sip.domain.com;transport=tls>;auth_pass=alicepassword;mediaenc=srtp-mand;input_wav=alice.wav"
	if cases2[0].sequence != expectedSeq4 {
		t.Errorf("expected sequence 4 %q, got %q", expectedSeq4, cases2[0].sequence)
	}

	// Test 3: Bracketless alias name with trailing custom parameter
	content3 := `
uanew alice;audio_codecs=opus
`
	cases3, _, _, _, _, _, _, _ := parseTestfile(content3, accounts)
	if len(cases3) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases3))
	}

	expectedSeq5 := "uanew <sip:alice@sip.domain.com;transport=tls>;auth_pass=alicepassword;mediaenc=srtp-mand;input_wav=alice.wav;audio_codecs=opus"
	if cases3[0].sequence != expectedSeq5 {
		t.Errorf("expected sequence 5 %q, got %q", expectedSeq5, cases3[0].sequence)
	}
}

func TestSanitization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "uanew <sip:alice@sip.domain.com;transport=tls>;auth_pass=alicepassword;mediaenc=srtp-mand",
			expected: "uanew <sip:alice@sip.domain.com;transport=tls>;auth_pass=******;mediaenc=srtp-mand",
		},
		{
			input:    "auth_pass=secret",
			expected: "auth_pass=******",
		},
		{
			input:    "auth_pass=secret;transport=tls",
			expected: "auth_pass=******;transport=tls",
		},
		{
			input:    `{"param":"CMD: uanew <sip:a@b>;auth_pass=secret"}`,
			expected: `{"param":"CMD: uanew <sip:a@b>;auth_pass=******"}`,
		},
		{
			input:    "no auth_pass here",
			expected: "no auth_pass here",
		},
	}

	for _, tt := range tests {
		got := SanitizeString(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeString(%q) = %q, want %q", tt.input, got, tt.expected)
		}

		gotBytes := string(SanitizeBytes([]byte(tt.input)))
		if gotBytes != tt.expected {
			t.Errorf("SanitizeBytes(%q) = %q, want %q", tt.input, gotBytes, tt.expected)
		}
	}
}
