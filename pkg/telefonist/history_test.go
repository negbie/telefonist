package telefonist

import (
	"bytes"
	"testing"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)

	// Test initial state
	if len(rb.GetAll()) != 0 {
		t.Errorf("Expected empty history, got %v", rb.GetAll())
	}

	// Add elements up to limit
	rb.Add([]byte("msg1"))
	rb.Add([]byte("msg2"))
	rb.Add([]byte("msg3"))

	history := rb.GetAll()
	if len(history) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(history))
	}
	if !bytes.Equal(history[0], []byte("msg1")) || !bytes.Equal(history[2], []byte("msg3")) {
		t.Errorf("Unexpected history order after 3 adds: %v", history)
	}

	// Overwrite oldest element
	rb.Add([]byte("msg4"))
	history = rb.GetAll()
	if len(history) != 3 {
		t.Errorf("Expected 3 messages after overwrite, got %d", len(history))
	}
	if !bytes.Equal(history[0], []byte("msg2")) || !bytes.Equal(history[2], []byte("msg4")) {
		t.Errorf("Unexpected history order after 4 adds: %v", history)
	}

	// Multiple overwrites
	rb.Add([]byte("msg5"))
	rb.Add([]byte("msg6"))
	history = rb.GetAll()
	if !bytes.Equal(history[0], []byte("msg4")) || !bytes.Equal(history[2], []byte("msg6")) {
		t.Errorf("Unexpected history order after 6 adds: %v", history)
	}
}

func TestRingBufferZeroLimit(t *testing.T) {
	rb := NewRingBuffer(0)
	rb.Add([]byte("msg1"))
	if len(rb.GetAll()) != 0 {
		t.Errorf("Expected empty history for zero limit, got %v", rb.GetAll())
	}
}
