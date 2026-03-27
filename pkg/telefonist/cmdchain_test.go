package telefonist

import (
	"testing"
	"time"
)

func TestIsDuration(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"10s", true},
		{"500ms", true},
		{"2m", true},
		{"0s", true},
		{"100ms", true},
		{"1m", true},
		// Invalid
		{"", false},
		{"10", false},
		{"s", false},
		{"abc", false},
		{"10xs", false},
		{"10 s", false},
		{"10S", false},
		{"hangup", false},
		{"dial 0123", false},
		{"10h", false},
		{"autodialgap=60", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isDuration(tt.input)
			if got != tt.want {
				t.Errorf("isDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsKnownCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"dial 0123", true},
		{"hangup", true},
		{"DIAL 0123", true},
		{"Hangup", true},
		{"transfer sip:foo@bar", true},
		{"listcalls", true},
		{"hold", true},
		{"resume", true},
		{"mute", true},
		{"train start", true},
		{"test abc123", true},
		{"accept", true},
		// Not known commands
		{"", false},
		{"autodialgap=60", false},
		{"transport=tcp", false},
		{"10s", false},
		{"foobar", false},
		{"unknown_cmd 123", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isKnownCommand(tt.input)
			if got != tt.want {
				t.Errorf("isKnownCommand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsChained(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple chain", "dial 0123|10s|hangup", true},
		{"chain with delay only", "dial 0123|10s", true},
		{"chain with command only", "dial 0123|hangup", true},
		{"multiple delays", "dial 0123|10s|transfer foo|500ms|hangup", true},
		{"no pipes", "dial 0123", false},
		{"empty", "", false},
		{"single command", "hangup", false},
		// Semicolons may appear inside parameters (SIP URIs, autodialadd params) and must NOT trigger chaining.
		{"autodialadd internal semicolon", "autodialadd 123456;autodialgap=60", false},
		// Pipe is the only chaining separator; this should chain even if the first command contains semicolons in params.
		{"autodialadd then command", "autodialadd 123456;autodialgap=60|hangup", true},
		{"sip uri with semicolon", "dial sip:user@host;transport=tcp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isChained(tt.input)
			if got != tt.want {
				t.Errorf("isChained(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseChain(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		tokens []chainToken
	}{
		{
			name:  "simple chain with delay",
			input: "dial 0123|10s|hangup",
			tokens: []chainToken{
				{command: "dial 0123"},
				{delay: 10 * time.Second, isDelay: true},
				{command: "hangup"},
			},
		},
		{
			name:  "multiple commands and delays",
			input: "dial 0123|10s|transfer foo|500ms|hangup",
			tokens: []chainToken{
				{command: "dial 0123"},
				{delay: 10 * time.Second, isDelay: true},
				{command: "transfer foo"},
				{delay: 500 * time.Millisecond, isDelay: true},
				{command: "hangup"},
			},
		},
		{
			name:  "back to back commands no delay",
			input: "dial 0123|hangup",
			tokens: []chainToken{
				{command: "dial 0123"},
				{command: "hangup"},
			},
		},
		{
			name:  "minute duration",
			input: "dial 0123|2m|hangup",
			tokens: []chainToken{
				{command: "dial 0123"},
				{delay: 2 * time.Minute, isDelay: true},
				{command: "hangup"},
			},
		},
		{
			name:  "preserves internal semicolons in autodialadd",
			input: "autodialadd 123456;autodialgap=60|10s|hangup",
			tokens: []chainToken{
				{command: "autodialadd 123456;autodialgap=60"},
				{delay: 10 * time.Second, isDelay: true},
				{command: "hangup"},
			},
		},
		{
			name:  "preserves sip uri semicolons",
			input: "dial sip:user@host;transport=tcp|10s|hangup",
			tokens: []chainToken{
				{command: "dial sip:user@host;transport=tcp"},
				{delay: 10 * time.Second, isDelay: true},
				{command: "hangup"},
			},
		},
		{
			name:  "single command no chain",
			input: "hangup",
			tokens: []chainToken{
				{command: "hangup"},
			},
		},
		{
			name:   "empty input",
			input:  "",
			tokens: nil,
		},
		{
			name:  "whitespace around tokens",
			input: " dial 0123 | 10s | hangup ",
			tokens: []chainToken{
				{command: "dial 0123"},
				{delay: 10 * time.Second, isDelay: true},
				{command: "hangup"},
			},
		},
		{
			name:  "delay at end",
			input: "dial 0123|10s|hangup|5s",
			tokens: []chainToken{
				{command: "dial 0123"},
				{delay: 10 * time.Second, isDelay: true},
				{command: "hangup"},
				{delay: 5 * time.Second, isDelay: true},
			},
		},
		{
			name:  "delay at start",
			input: "500ms|dial 0123",
			tokens: []chainToken{
				{delay: 500 * time.Millisecond, isDelay: true},
				{command: "dial 0123"},
			},
		},
		{
			name:  "consecutive delays",
			input: "dial 0123|2s|3s|hangup",
			tokens: []chainToken{
				{command: "dial 0123"},
				{delay: 2 * time.Second, isDelay: true},
				{delay: 3 * time.Second, isDelay: true},
				{command: "hangup"},
			},
		},
		{
			name:  "multiple internal semicolons preserved",
			input: "dial sip:user@host;transport=tcp;maddr=10.0.0.1|5s|hangup",
			tokens: []chainToken{
				{command: "dial sip:user@host;transport=tcp;maddr=10.0.0.1"},
				{delay: 5 * time.Second, isDelay: true},
				{command: "hangup"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseChain(tt.input)
			if len(got) != len(tt.tokens) {
				t.Fatalf("parseChain(%q) returned %d tokens, want %d\ngot:  %+v\nwant: %+v",
					tt.input, len(got), len(tt.tokens), got, tt.tokens)
			}
			for i := range got {
				if got[i].isDelay != tt.tokens[i].isDelay {
					t.Errorf("token[%d].isDelay = %v, want %v", i, got[i].isDelay, tt.tokens[i].isDelay)
				}
				if got[i].isDelay {
					if got[i].delay != tt.tokens[i].delay {
						t.Errorf("token[%d].delay = %v, want %v", i, got[i].delay, tt.tokens[i].delay)
					}
				} else {
					if got[i].command != tt.tokens[i].command {
						t.Errorf("token[%d].command = %q, want %q", i, got[i].command, tt.tokens[i].command)
					}
				}
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"10s", 10 * time.Second, false},
		{"500ms", 500 * time.Millisecond, false},
		{"2m", 2 * time.Minute, false},
		{"0s", 0, false},
		{"1s", 1 * time.Second, false},
		{"100ms", 100 * time.Millisecond, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := time.ParseDuration(tt.input)
			if (err != nil) != tt.err {
				t.Errorf("time.ParseDuration(%q) error = %v, wantErr = %v", tt.input, err, tt.err)
				return
			}
			if got != tt.want {
				t.Errorf("time.ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandCommand(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: ";wav=alice.wav/",
			want:  ";audio_source=aufile,data/sounds/alice.wav;audio_player=aufile,data/recorded_temp/",
		},
		{
			input: "uanew <ua1>;wav=bob.wav/",
			want:  "uanew <ua1>;audio_source=aufile,data/sounds/bob.wav;audio_player=aufile,data/recorded_temp/",
		},
		{
			input: "dial ua1;wav=charli.wav/",
			want:  "dial ua1;audio_source=aufile,data/sounds/charli.wav;audio_player=aufile,data/recorded_temp/",
		},
		{
			input: "no change",
			want:  "no change",
		},
		{
			input: ";wav=alice.wav/;wav=bob.wav/",
			want:  ";audio_source=aufile,data/sounds/alice.wav;audio_player=aufile,data/recorded_temp/;audio_source=aufile,data/sounds/bob.wav;audio_player=aufile,data/recorded_temp/",
		},
		{
			input: "uanew <sip:user@host;transport=tls>;auth_pass=123",
			want:  "uanew <sip:user@host;transport=tls>;auth_pass=123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandCommand(tt.input, "data")
			if got != tt.want {
				t.Errorf("expandCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
