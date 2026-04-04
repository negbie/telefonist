package telefonist

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/grafana/sobek"
)

// Evaluator manages the Sobek JS runtime setup and execution for _include tests.
type Evaluator struct {
	vm *sobek.Runtime
}

// NewEvaluator creates a new JS evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		vm: sobek.New(),
	}
}

// RunScript parses the full log output from TrainSession, constructs the
// events and stats objects, injects the telefonist helper, and runs the script.
func (e *Evaluator) RunScript(ctx context.Context, script string, rawLogOutput string) error {
	lines := strings.Split(rawLogOutput, "\n\n")
	
	var events []map[string]interface{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(line), &evt); err == nil {
			events = append(events, evt)
		}
	}

	// Make events array available
	e.vm.Set("events", events)

	// Make a mock stats object available for now (could be populated from events)
	e.vm.Set("stats", map[string]interface{}{})

	// Setup the telefonist utility functions
	telefonistObj := e.vm.NewObject()
	telefonistObj.Set("assert", func(call sobek.FunctionCall) sobek.Value {
		if len(call.Arguments) < 1 {
			panic(e.vm.ToValue("assert requires at least 1 argument condition"))
		}
		cond := call.Arguments[0].ToBoolean()
		if !cond {
			reason := "assertion failed"
			if len(call.Arguments) >= 2 {
				reason = call.Arguments[1].String()
			}
			panic(e.vm.ToValue(reason))
		}
		return sobek.Undefined()
	})
	telefonistObj.Set("fail", func(call sobek.FunctionCall) sobek.Value {
		reason := "test explicitly failed"
		if len(call.Arguments) >= 1 {
			reason = call.Arguments[0].String()
		}
		if true {
			panic(e.vm.ToValue(reason))
		}
		return sobek.Undefined()
	})
	e.vm.Set("telefonist", telefonistObj)

	log.Printf("Evaluating _include script with %d events", len(events))

	// Enforce execution timeout or cancellation to prevent infinite loops (DoS)
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			e.vm.Interrupt("Evaluation cancelled or timed out")
		case <-done:
		}
	}()

	_, err := e.vm.RunString(script)
	if err != nil {
		if jsErr, ok := err.(*sobek.Exception); ok {
			// Extract just the error message without the full stack trace if it's a simple panic
			return fmt.Errorf("JS evaluation failed: %v", jsErr.Value())
		}
		return fmt.Errorf("JS evaluation error: %v", err)
	}

	return nil
}
