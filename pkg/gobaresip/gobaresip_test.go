package gobaresip

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
)


func TestReadNetstring(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "5:hello,", "hello", false},
		{"json", `28:{"command":"dial","ok":true},`, `{"command":"dial","ok":true}`, false},
		{"zero length", "0:,", "", true},
		{"negative", "-1:x,", "", true},
		{"bad chars", "abc:xxx,", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newReader(strings.NewReader(tt.input))
			got, err := r.readNetstring()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (data=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNetstringFormatting(t *testing.T) {
	// Verify our manual netstring assembly matches the expected format.
	msg := []byte(`{"command":"dial","params":"100"}`)
	lenStr := strconv.Itoa(len(msg))
	buf := make([]byte, 0, len(lenStr)+1+len(msg)+1)
	buf = append(buf, lenStr...)
	buf = append(buf, ':')
	buf = append(buf, msg...)
	buf = append(buf, ',')

	want := `33:{"command":"dial","params":"100"},`
	if string(buf) != want {
		t.Errorf("netstring format:\n got  %q\n want %q", string(buf), want)
	}

	// Round-trip: parse what we just built.
	r := newReader(bytes.NewReader(buf))
	got, err := r.readNetstring()
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, msg)
	}
}

func TestCmdWsSentinelError(t *testing.T) {
	// We can't run a full Baresip for CmdWs, but we can test the
	// sentinel error paths that don't touch the TCP connection.
	b := &Baresip{}

	tests := []struct {
		name string
		raw  []byte
	}{
		{"empty", []byte("")},
		{"whitespace", []byte("   ")},
		{"quit", []byte("quit")},
		{"QUIT mixed case", []byte("QUIT")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.CmdWs(tt.raw)
			if !errors.Is(err, ErrIgnoredCommand) {
				t.Errorf("CmdWs(%q) = %v, want ErrIgnoredCommand", tt.raw, err)
			}
		})
	}
}
