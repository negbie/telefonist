package telefonist

import (
	"testing"
	"time"
)

// TestRace_WsHub_ActiveAgent demonstrates the race condition on h.activeAgent
// occurring under normal WebSocket usage:
// When a chained command (e.g. "dial 100|1ms|hangup") is received via WebSocket,
// WsHub.Run spawns a background goroutine to execute the chain via executeChain.
// Meanwhile, incoming single commands (e.g. "uafind ua1", "dial 200", "uadelall")
// are handled directly on the WsHub.Run loop by calling executeSmartCommand.
// Both goroutines read and write h.activeAgent concurrently without synchronization.
func TestRace_WsHub_ActiveAgent(t *testing.T) {
	tempDir := t.TempDir()
	hub := NewWsHub(tempDir, 1, "", "", 0, false, "")
	go hub.Run()
	defer hub.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 40; i++ {
			// Client 1 sends a chained command (runs asynchronously via executeChain)
			hub.command <- []byte("dial 100|1ms|hangup")
			// Client 2 sends unchained commands (runs synchronously on WsHub.Run loop)
			hub.command <- []byte("uafind ua1")
			hub.command <- []byte("dial 200")
			time.Sleep(1 * time.Millisecond)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out")
	}
}
