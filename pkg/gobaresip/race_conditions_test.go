package gobaresip

import (
	"net"
	"sync"
	"testing"
	"time"
)

// TestRace_Baresip_CloseVsMsgChanSend demonstrates the data race and runtime panic
// ("send on closed channel") occurring during shutdown when Baresip.Close() closes
// b.msgChan while CGO handlers (gobaresip_log_handler, gobaresip_sip_trace_handler,
// gobaresip_ui_output_handler) are delivering messages from the baresip C core.
func TestRace_Baresip_CloseVsMsgChanSend(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		b := &Baresip{
			msgChan: make(chan Msg, 100),
			closeCh: make(chan struct{}),
		}
		activeBaresipPtr.Store(b)

		var wg sync.WaitGroup
		wg.Add(2)

		// Simulates CGO callback thread delivering baresip log/trace messages
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				inst := activeBaresipPtr.Load()
				if inst != nil {
					inst.sendMsg(Msg{Log: "SIP trace or log message"})
				}
				time.Sleep(10 * time.Microsecond)
			}
		}()

		// Simulates agent termination / shutdown calling Close()
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Microsecond)
			b.Close()
		}()

		wg.Wait()
	}
}

// TestRace_Baresip_ReconnectVsCmd demonstrates the data race on b.ctrlConn
// when tryReconnect() assigns a new b.ctrlConn in the background read() goroutine
// without holding writeMu, while b.Cmd() reads b.ctrlConn under writeMu.
func TestRace_Baresip_ReconnectVsCmd(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Accept connections and close them shortly after to trigger reconnect
	stopAccept := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				time.Sleep(2 * time.Millisecond)
				_ = c.Close()
			}(conn)
			select {
			case <-stopAccept:
				return
			default:
			}
		}
	}()
	defer close(stopAccept)

	b, err := New(
		SetRemote(true),
		SetCtrlTCPAddr(ln.Addr().String()),
		SetReconnect(1*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = b.Cmd("callstat", "", "token")
			time.Sleep(100 * time.Microsecond)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("test timed out")
	}
}
