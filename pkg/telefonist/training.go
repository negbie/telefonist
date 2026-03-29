package telefonist

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

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
	ignoredEvents []string
	acceptedEvents []string
}

// newTrainSession creates a new testing session.
func newTrainSession(ignoredEvents []string, acceptedEvents []string) *TrainSession {
	return &TrainSession{
		active:         true,
		ignoredEvents:  ignoredEvents,
		acceptedEvents: acceptedEvents,
	}
}

var idRegex = regexp.MustCompile(`"id":"[^"]+"`)

// Must only be called from the hub's run loop.
func (t *TrainSession) recordEvent(e gobaresip.EventMsg) {
	if !t.active {
		return
	}

	// Always record to fullBuf for storage/download
	t.fullBuf.Write(e.RawJSON)
	t.fullBuf.WriteString("\n\n")

	// Following types should be excluded from the hash calculation
	if e.Type == "CALL_RTCP" || e.Type == "SIP" || e.Type == "LOG" {
		return
	}

	for _, ignored := range t.ignoredEvents {
		if strings.EqualFold(e.Type, ignored) {
			return
		}
	}

	if len(t.acceptedEvents) > 0 {
		found := false
		for _, accepted := range t.acceptedEvents {
			if strings.EqualFold(e.Type, accepted) {
				found = true
				break
			}
		}
		if !found {
			return
		}
	}

	// Strip the dynamic "id" field to maintain a deterministic hash
	strippedJSON := idRegex.ReplaceAllString(string(e.RawJSON), `"id":""`)
	extracted := strippedJSON + "\n\n"

	t.outputBuf.WriteString(extracted)
}

// Must only be called from the hub's run loop.
func (t *TrainSession) recordRaw(msg []byte) {
	if !t.active {
		return
	}
	// Raw messages (like SIP/LOG) go ONLY to fullBuf to keep hash clean.
	// We also skip action events and command hints to keep them out of the compare view/hash.
	if bytes.Contains(msg, []byte(`"action":`)) || bytes.Contains(msg, []byte(`"type":"CMD"`)) {
		return
	}
	t.fullBuf.Write(msg)
	t.fullBuf.WriteString("\n\n")
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

// sessionIsActive reports whether the session is non-nil and active.
// Must only be called from the hub's run loop.
func sessionIsActive(session *TrainSession) bool {
	if session == nil {
		return false
	}
	return session.active
}

func hashOutput(s string) string {
	return fmt.Sprintf("%016x", xxhash.Sum64String(s))
}
