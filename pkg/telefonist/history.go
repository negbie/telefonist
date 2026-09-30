package telefonist

import (
	"bytes"
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
	bytes  int
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

	r.bytes -= len(r.data[r.cursor])
	r.data[r.cursor] = msg
	r.bytes += len(msg)
	r.cursor++
	if r.cursor >= r.limit {
		r.cursor = 0
		r.full = true
	}
	for i := 0; r.bytes > 32*1024*1024 && i < r.limit; i++ {
		index := (r.cursor + i) % r.limit
		r.bytes -= len(r.data[index])
		r.data[index] = nil
	}
}

// Snapshot returns the current messages from oldest to newest. The returned
// slice is independent from the ring; message byte slices are shared because
// history messages are immutable after insertion.
func (r *RingBuffer) Snapshot() [][]byte {
	if r.limit <= 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := r.cursor
	if r.full {
		count = r.limit
	}
	out := make([][]byte, 0, count)

	if !r.full {
		for _, msg := range r.data[:r.cursor] {
			if msg != nil {
				out = append(out, msg)
			}
		}
		return out
	}

	// Buffer is full; replay [cursor:limit] then [0:cursor]
	for _, msg := range r.data[r.cursor:r.limit] {
		if msg != nil {
			out = append(out, msg)
		}
	}
	for _, msg := range r.data[:r.cursor] {
		if msg != nil {
			out = append(out, msg)
		}
	}
	return out
}

// ReplayTo streams all messages in the buffer to the provided channel.
// Order is preserved from oldest to newest. Non-blocking send ensures
// that slower consumers don't block the replayer.
func (r *RingBuffer) ReplayTo(dest chan []byte) {
	for _, msg := range r.Snapshot() {
		select {
		case dest <- msg:
		default:
			// buffer full, skip to keep replayer moving
		}
	}
}

// ReplayBatchesTo snapshots history and replays it as newline-delimited batches.
// This reduces channel and WebSocket frame overhead for new clients while keeping
// the same per-line payload format consumed by the browser.
func (r *RingBuffer) ReplayBatchesTo(dest chan []byte, maxMessages int, maxBytes int) {
	messages := r.Snapshot()
	if len(messages) == 0 {
		return
	}
	if maxMessages <= 0 {
		maxMessages = len(messages)
	}
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024
	}

	var batch bytes.Buffer
	batchCount := 0
	flush := func() bool {
		if batchCount == 0 {
			return true
		}
		payload := append([]byte(nil), batch.Bytes()...)
		select {
		case dest <- payload:
			batch.Reset()
			batchCount = 0
			return true
		default:
			return false
		}
	}

	for _, msg := range messages {
		if batchCount > 0 && (batchCount >= maxMessages || batch.Len()+1+len(msg) > maxBytes) {
			if !flush() {
				return
			}
		}
		if batchCount > 0 {
			batch.WriteByte('\n')
		}
		batch.Write(msg)
		batchCount++
	}
	flush()
}
