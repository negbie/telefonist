package telefonist

import (
	"sync"
)

// RingBuffer is a thread-safe ring buffer for storing message history.
// It maintains a fixed-size window of recent messages, dropping the oldest
// when the limit is exceeded.
type RingBuffer struct {
	mu     sync.RWMutex
	data   [][]byte
	limit  int
	cursor int // points to the position for the next message
	full   bool
}

// NewRingBuffer creates a new RingBuffer with the specified message limit.
func NewRingBuffer(limit int) *RingBuffer {
	if limit <= 0 {
		return &RingBuffer{limit: 0}
	}
	return &RingBuffer{
		data:  make([][]byte, limit),
		limit: limit,
	}
}

// Add appends a new message to the buffer, overwriting the oldest if at capacity.
func (r *RingBuffer) Add(msg []byte) {
	if r.limit <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data[r.cursor] = msg
	r.cursor++
	if r.cursor >= r.limit {
		r.cursor = 0
		r.full = true
	}
}

// GetAll returns a copy of all messages in the buffer, ordered from oldest to newest.
func (r *RingBuffer) GetAll() [][]byte {
	if r.limit <= 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.full {
		// Buffer isn't full yet; return from 0 to cursor
		res := make([][]byte, r.cursor)
		copy(res, r.data[:r.cursor])
		return res
	}

	// Buffer is full; return in order [cursor:limit] followed by [0:cursor]
	res := make([][]byte, r.limit)
	n := copy(res, r.data[r.cursor:])
	copy(res[n:], r.data[:r.cursor])
	return res
}
