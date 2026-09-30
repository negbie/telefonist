package telefonist

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEvaluatorAssertionsUseRecordedEvents(t *testing.T) {
	const script = `
telefonist.assert(events.length === 2, "missing events");
telefonist.assert(events[0].type === "CALL_ESTABLISHED", "call never established");
telefonist.assert(events[1].type === "CALL_CLOSED", "call never closed");
telefonist.assert(events[0]._agent === "alice", "wrong agent");
`
	for _, tc := range []struct {
		name, log, wantError string
	}{
		{"completed call", `{"type":"CALL_ESTABLISHED","_agent":"alice"}` + "\n\n" + `{"type":"CALL_CLOSED"}`, ""},
		{"missing close", `{"type":"CALL_ESTABLISHED","_agent":"alice"}`, "missing events"},
		{"wrong order", `{"type":"CALL_CLOSED"}` + "\n\n" + `{"type":"CALL_ESTABLISHED","_agent":"alice"}`, "call never established"},
		{"wrong agent", `{"type":"CALL_ESTABLISHED","_agent":"bob"}` + "\n\n" + `{"type":"CALL_CLOSED"}`, "wrong agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := NewEvaluator().RunScript(context.Background(), script, tc.log)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("got %v, want failure containing %q", err, tc.wantError)
			}
		})
	}
}

func TestEvaluatorCancellationInterruptsInfiniteScript(t *testing.T) {
	e := NewEvaluator()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	if err := e.vm.Set("signalStarted", func() { close(started) }); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.RunScript(ctx, "signalStarted(); for (;;) {}", "") }()
	// Ensure cancellation happens during execution, rather than before startup.
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("script did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("cancelled script returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not interrupt script")
	}
}
