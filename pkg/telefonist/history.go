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

// ReplayTo streams all messages in the buffer to the provided channel.
// Order is preserved from oldest to newest. Non-blocking send ensures
// that slower consumers don't block the replayer.
func (r *RingBuffer) ReplayTo(dest chan []byte) {
	if r.limit <= 0 {
		return
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.full {
		for i := 0; i < r.cursor; i++ {
			select {
			case dest <- r.data[i]:
			default:
				// buffer full, skip to keep replayer moving
			}
		}
		return
	}

	// Buffer is full; replay [cursor:limit] then [0:cursor]
	for i := r.cursor; i < r.limit; i++ {
		select {
		case dest <- r.data[i]:
		default:
		}
	}
	for i := 0; i < r.cursor; i++ {
		select {
		case dest <- r.data[i]:
		default:
		}
	}
}
