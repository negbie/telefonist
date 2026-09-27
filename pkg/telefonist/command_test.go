package telefonist

import (
	"testing"
)

func TestResolveTarget(t *testing.T) {
	bm := &BaresipManager{
		agents: make(map[string]*Agent),
	}
	bm.agents["sip:test1@host.com"] = &Agent{}
	bm.agents["ua1"] = &Agent{}

	tests := []struct {
		name           string
		cmd            string
		fallback       string
		expectedTarget string
		expectedCmd    string
	}{
		{
			"Simple alias prefix",
			"ua1:dial destination",
			"ua2",
			"ua1",
			"dial destination",
		},
		{
			"Full AOR prefix",
			"sip:test1@host.com:accept",
			"ua2",
			"sip:test1@host.com",
			"accept",
		},
		{
			"Prefix with transport parameter",
			"sip:test1@host.com;transport=tls:dial destination",
			"ua2",
			"sip:test1@host.com",
			"dial destination",
		},
		{
			"No prefix, use fallback",
			"dial destination",
			"ua1",
			"ua1",
			"dial destination",
		},
		{
			"Command containing colons (no prefix)",
			"dial sip:other@host ;wav=test.wav/",
			"ua1",
			"ua1",
			"dial sip:other@host ;wav=test.wav/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, cleaned := bm.ResolveTarget(tt.cmd, tt.fallback)
			if target != tt.expectedTarget {
				t.Errorf("ResolveTarget() target = %v, want %v", target, tt.expectedTarget)
			}
			if cleaned != tt.expectedCmd {
				t.Errorf("ResolveTarget() cleanedCmd = %v, want %v", cleaned, tt.expectedCmd)
			}
		})
	}
}

func TestExpandShortcuts(t *testing.T) {
	soundsDir := "/abs/sounds"
	recordsDir := "/abs/records"

	tests := []struct {
		name     string
		cmd      string
		expected string
	}{
		{
			"Do not expand ;wav= outside account setup",
			"dial dest ;wav=hello.wav/",
			"dial dest ;wav=hello.wav/",
		},
		{
			"Expand ;input_wav= (in account string, no space)",
			"uanew <sip:t@h>;auth=p;input_wav=alice.wav",
			"uanew <sip:t@h>;auth=p;audio_source=aufile,/abs/sounds/alice.wav",
		},
		{
			"Do not expand wav= outside account setup",
			"dial destwav=test.wav/",
			"dial destwav=test.wav/",
		},
		{
			"Do not expand input_wav= outside account setup",
			"dial dest input_wav=test.wav",
			"dial dest input_wav=test.wav",
		},
		{
			"Do not expand mixed-case wav outside account setup",
			"dial dest ;WAV=test.wav/",
			"dial dest ;WAV=test.wav/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandShortcuts(tt.cmd, soundsDir, recordsDir)
			if got != tt.expected {
				t.Errorf("ExpandShortcuts() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestHasAuthPass(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
	}{
		{"", false},
		{"auth_pas", false},
		{"auth_pass", true},
		{"AUTH_PASS", true},
		{"aUtH_pAsS", true},
		{"uanew <sip:alice@domain>;auth_pass=secret", true},
		{"reg: Auth_Pass=1234", true},
		{"no password here", false},
	}
	for _, tc := range cases {
		got := HasAuthPass(tc.input)
		if got != tc.expected {
			t.Errorf("HasAuthPass(%q) = %v; want %v", tc.input, got, tc.expected)
		}
		gotBytes := HasAuthPassBytes([]byte(tc.input))
		if gotBytes != tc.expected {
			t.Errorf("HasAuthPassBytes(%q) = %v; want %v", tc.input, gotBytes, tc.expected)
		}
	}
}

func TestSanitizeString(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"hello world", "hello world"},
		{"auth_pass=secret;other=1", "auth_pass=******;other=1"},
		{"AUTH_PASS=secret123", "AUTH_PASS=******"},
	}
	for _, tc := range cases {
		got := SanitizeString(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeString(%q) = %q; want %q", tc.input, got, tc.expected)
		}
		gotBytes := string(SanitizeBytes([]byte(tc.input)))
		if gotBytes != tc.expected {
			t.Errorf("SanitizeBytes(%q) = %q; want %q", tc.input, gotBytes, tc.expected)
		}
	}
}
