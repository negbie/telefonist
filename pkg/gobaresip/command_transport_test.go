package gobaresip

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentCommandsArriveAsCompleteNetstrings(t *testing.T) {
	writer, peer := net.Pipe()
	defer writer.Close()
	defer peer.Close()
	if err := peer.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	b := &Baresip{ctrlConn: writer, remote: true, ctx: context.Background()}
	atomic.StoreUint32(&b.ctrlConnAlive, 1)
	commands := []CommandMsg{
		{Command: "dial", Params: "sip:alice@example.test", Token: "first"},
		{Command: "message", Params: "Grüße 世界", Token: "second"},
		{Command: "hangup", Token: "third"},
	}
	errors := make(chan error, len(commands))
	var wg sync.WaitGroup
	for _, command := range commands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- b.Cmd(command.Command, command.Params, command.Token)
		}()
	}
	got := make(map[string]CommandMsg)
	r := newReader(peer)
	for range commands {
		frame, err := r.readNetstring()
		if err != nil {
			t.Fatal(err)
		}
		var command CommandMsg
		if err := json.Unmarshal(frame, &command); err != nil {
			t.Fatalf("corrupt command frame: %v", err)
		}
		if _, duplicate := got[command.Token]; duplicate {
			t.Fatalf("duplicate command token %q", command.Token)
		}
		got[command.Token] = command
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range commands {
		if got[want.Token] != want {
			t.Fatalf("received %+v, want %+v", got[want.Token], want)
		}
	}
}

func TestCommandReportsBrokenTransport(t *testing.T) {
	writer, peer := net.Pipe()
	defer writer.Close()
	_ = peer.Close()
	b := &Baresip{ctrlConn: writer, remote: true, ctx: context.Background()}
	atomic.StoreUint32(&b.ctrlConnAlive, 1)
	if err := b.Cmd("dial", "sip:alice@example.test", "test"); err == nil {
		t.Fatal("command on a broken transport reported success")
	}
}
