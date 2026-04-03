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
			"Expand ;wav= (standalone with space)",
			"dial dest ;wav=hello.wav/",
			"dial dest ;audio_source=aufile,/abs/sounds/hello.wav;audio_player=aufile,/abs/records/",
		},
		{
			"Expand ;input_wav= (in account string, no space)",
			"uanew <sip:t@h>;auth=p;input_wav=alice.wav",
			"uanew <sip:t@h>;auth=p;audio_source=aufile,/abs/sounds/alice.wav",
		},
		{
			"Expand wav= (no semi, no space)",
			"dial destwav=test.wav/",
			"dial dest;audio_source=aufile,/abs/sounds/test.wav;audio_player=aufile,/abs/records/",
		},
		{
			"Expand input_wav= with no semicolon prefix",
			"dial dest input_wav=test.wav",
			"dial dest ;audio_source=aufile,/abs/sounds/test.wav",
		},
		{
			"Mixed case expansion",
			"dial dest ;WAV=test.wav/",
			"dial dest ;audio_source=aufile,/abs/sounds/test.wav;audio_player=aufile,/abs/records/",
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
