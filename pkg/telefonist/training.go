package telefonist

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

// OrderIndependentHash computes a hash by summing up the xxhash of each line individually.
// This ensures that the exact order of events doesn't change the final hash, while being
// much more precise than counting characters.
// It also returns the alphabetically sorted list of formatted lines it used for hashing
// so the UI can construct a cleanly diffable test event sequence block for debugging.
func OrderIndependentHash(s string) (string, []string) {
	var totalSum uint64

	lines := strings.Split(s, "\n\n")
	var sortedLines []string

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Hash each individual event/line
		h := xxhash.Sum64String(line)
		totalSum += h

		sortedLines = append(sortedLines, line)
	}

	sort.Strings(sortedLines)

	return fmt.Sprintf("%016x", totalSum), sortedLines
}

// TrainSession captures commands and WebSocket output during a testing
// run so that the output can be hashed and compared later.
//
// NOTE: TrainSession is NOT thread-safe. All access must be serialized
// through the WsHub's internalCmd channel (actor/monitor pattern).
// Do NOT add a mutex here — the hub's single goroutine owns this value.
type TrainSession struct {
	active        bool
	outputBuf     bytes.Buffer // For hashing (filtered/clean)
	fullBuf       bytes.Buffer // For downloads/storage (raw)
	ignored       map[string]bool
	accepted      map[string]bool // nil = accept all
	failMsg       string
	runID         int64
	started       time.Time
	eventSequence uint64
}

// newTrainSession creates a new testing session.
func newTrainSession(ignoredEvents []string, acceptedEvents []string) *TrainSession {
	ignored := make(map[string]bool, len(ignoredEvents))
	for _, e := range ignoredEvents {
		ignored[strings.ToUpper(e)] = true
	}
	accepted := make(map[string]bool, len(acceptedEvents))
	for _, e := range acceptedEvents {
		accepted[strings.ToUpper(e)] = true
	}
	if len(accepted) == 0 {
		accepted = nil
	}
	return &TrainSession{
		active:   true,
		started:  time.Now(),
		ignored:  ignored,
		accepted: accepted,
	}
}

// Evidence may contain timestamps and run metadata; it never feeds the hash.
func (t *TrainSession) recordEvidence(raw []byte) {
	if !t.active {
		return
	}
	// Always record to fullBuf for storage/download
	if t.fullBuf.Len()+len(raw)+2 > 32*1024*1024 {
		t.failMsg = "telemetry exceeded 32 MiB run limit"
		return
	}
	t.fullBuf.Write(raw)
	t.fullBuf.WriteString("\n\n")
}

// Hash the original agent event, before enrichment or reserialization.
func (t *TrainSession) recordHashEvent(e gobaresip.EventMsg) {
	if !t.active {
		return
	}
	if e.Type == "REGISTER_FAIL" {
		t.failMsg = "Registration failed"
	}

	// Exclude non-deterministic and lifecycle event types from hash calculation
	switch e.Type {
	case "CALL_RTCP", "SIP", "LOG", "SHUTDOWN", "UNREGISTERING", "EXIT", "RESPONSE", "MODULE", "END_OF_FILE", "NATIVE_DONE":
		return
	}

	if t.ignored[strings.ToUpper(e.Type)] {
		return
	}

	if t.accepted != nil && !t.accepted[strings.ToUpper(e.Type)] {
		return
	}

	stripped := replaceFieldBytes(e.RawJSON, []byte(`"id":"`), nil)
	stripped = replaceFieldBytes(stripped, []byte(`"contacturi":"`), nil)

	if e.Type != "AUDIO_REPORT" && e.Type != "PRESENCE" {
		stripped = replaceFieldBytes(stripped, []byte(`"param":"`), nil)
	}

	t.outputBuf.Write(stripped)
	t.outputBuf.WriteString("\n\n")
}

// finish ends the training session and returns the output hash for deterministic events.
// Must only be called from the hub's run loop.
func (t *TrainSession) finish() (outputHash string) {
	if !t.active {
		// Already finished.
		hash, _ := OrderIndependentHash(t.outputBuf.String())
		return hashOutput(hash)
	}

	t.active = false

	hash, _ := OrderIndependentHash(t.outputBuf.String())
	outputHash = hashOutput(hash)

	return outputHash
}

// GetFullOutput returns the complete, unfiltered session log.
func (t *TrainSession) GetFullOutput() string {
	return t.fullBuf.String()
}

func hashOutput(s string) string {
	return fmt.Sprintf("%016x", xxhash.Sum64String(s))
}

// Preserve the historical bytes, including leftover commas: existing baselines
// depend on them. This is hash input, not JSON for display or storage.
func replaceFieldBytes(b []byte, prefix []byte, replacement []byte) []byte {
	idx := bytes.Index(b, prefix)
	if idx == -1 {
		return b
	}

	var res []byte
	for idx != -1 {
		valStart := idx + len(prefix)
		endQuote := bytes.IndexByte(b[valStart:], '"')
		if endQuote == -1 {
			break
		}
		valEnd := valStart + endQuote + 1

		if res == nil {
			res = make([]byte, 0, len(b))
		}
		res = append(res, b[:idx]...)
		if len(replacement) > 0 {
			res = append(res, replacement...)
		}
		b = b[valEnd:]
		idx = bytes.Index(b, prefix)
	}

	if res == nil {
		return b
	}
	res = append(res, b...)
	return res
}
