package gobaresip

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcknowledgementUsesOpaqueTokenAndRejectsFailure(t *testing.T) {
	master, agent := net.Pipe()
	defer agent.Close()
	b := &Baresip{ctrlConn: master, ctrlStream: newReader(master), remote: true, ctx: context.Background(), closeCh: make(chan struct{}), readDone: make(chan struct{}), msgChan: make(chan Msg, 4)}
	atomic.StoreUint32(&b.ctrlConnAlive, 1)
	b.readWG.Add(1)
	go b.read()
	defer b.Close()
	result := make(chan error, 1)
	go func() { result <- b.CmdWsContext(context.Background(), []byte("uanew sip:alice;auth_pass=secret")) }()
	agent.SetDeadline(time.Now().Add(time.Second))
	raw, err := newReader(agent).readNetstring()
	if err != nil {
		t.Fatal(err)
	}
	var command CommandMsg
	if err = json.Unmarshal(raw, &command); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(command.Token, "secret") || !strings.HasPrefix(command.Token, "request_") {
		t.Fatal("unsafe correlation token", command.Token)
	}
	response, _ := json.Marshal(ResponseMsg{Response: true, Ok: false, Data: "registration rejected", Token: command.Token})
	if _, err = fmt.Fprintf(agent, "%d:%s,", len(response), response); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if err == nil || !strings.Contains(err.Error(), "rejected") {
			t.Fatal("negative acknowledgement accepted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("acknowledgement not delivered")
	}
}

func FuzzNetstringBounded(f *testing.F) {
	for _, input := range []string{"2:{},", "0:,", "999999999999999:", "-1:x,", "262145:", "01:x,", "2:x,", "2:{};"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		payload, err := newReader(strings.NewReader(input)).readNetstring()
		if err == nil && (len(payload) == 0 || len(payload) > maxNetstringSize) {
			t.Fatal("invalid accepted length", len(payload))
		}
	})
}
