package telefonist

import (
	"bytes"
	"testing"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)

	// Test initial state
	dest := make(chan []byte, 3)
	rb.ReplayTo(dest)
	if len(dest) != 0 {
		t.Errorf("Expected empty history, got %d messages", len(dest))
	}

	// Add elements up to limit
	rb.Add([]byte("msg1"))
	rb.Add([]byte("msg2"))
	rb.Add([]byte("msg3"))

	dest = make(chan []byte, 3)
	rb.ReplayTo(dest)
	if len(dest) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(dest))
	}
	if !bytes.Equal(<-dest, []byte("msg1")) || !bytes.Equal(<-dest, []byte("msg2")) || !bytes.Equal(<-dest, []byte("msg3")) {
		t.Errorf("Unexpected history order after 3 adds")
	}

	// Overwrite oldest element
	rb.Add([]byte("msg4"))
	dest = make(chan []byte, 3)
	rb.ReplayTo(dest)
	if len(dest) != 3 {
		t.Fatalf("Expected 3 messages after overwrite, got %d", len(dest))
	}
	if !bytes.Equal(<-dest, []byte("msg2")) || !bytes.Equal(<-dest, []byte("msg3")) || !bytes.Equal(<-dest, []byte("msg4")) {
		t.Errorf("Unexpected history order after 4 adds")
	}

	// Multiple overwrites
	rb.Add([]byte("msg5"))
	rb.Add([]byte("msg6"))
	dest = make(chan []byte, 3)
	rb.ReplayTo(dest)
	if !bytes.Equal(<-dest, []byte("msg4")) || !bytes.Equal(<-dest, []byte("msg5")) || !bytes.Equal(<-dest, []byte("msg6")) {
		t.Errorf("Unexpected history order after 6 adds")
	}
}

func TestRingBufferZeroLimit(t *testing.T) {
	rb := NewRingBuffer(0)
	rb.Add([]byte("msg1"))
	dest := make(chan []byte, 1)
	rb.ReplayTo(dest)
	if len(dest) != 0 {
		t.Errorf("Expected empty history for zero limit, got %d messages", len(dest))
	}
}
