package telefonist

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

// WsHub maintains the set of active clients and broadcasts events to the clients.
type WsHub struct {
	// Registered clients.
	clients map[*client]bool

	// Inbound command from the clients.
	command chan []byte

	// Register requests from the clients.
	register chan *client

	// Unregister requests from clients.
	unregister chan *client

	// Outbound broadcast messages (serialized through the hub).
	broadcast chan []byte

	bs *gobaresip.Baresip

	// Callback handlers
	onEvent    func(gobaresip.EventMsg)
	onResponse func(gobaresip.ResponseMsg)

	// Persistent testfile store (SQLite, next to executable)
	testStore *TestStore

	// Training/testing session state (owned by run loop)
	trainSession *TrainSession

	// Inline testfile run guard (prevents queueing)
	inlineRunActive atomic.Bool

	// Cancellation for the current test run
	testCancel context.CancelFunc

	// Waiters for barrier synchronization
	syncWaiters map[string]chan struct{}

	ctx    context.Context
	cancel context.CancelFunc

	internalCmd chan func()
	callIDs     sync.Map // map[string]string: Localuri -> ID

	// chainMu ensures only one executeChain runs at a time,
	// preventing interleaving of multi-step command sequences.
	chainMu sync.Mutex

	DataDir string
	skipSipMethods []string

	cmdCounter atomic.Uint64
}

// broadcastToClients sends msg to every connected client.
// Slow clients that can't keep up are disconnected.
// Must only be called from the hub's run() goroutine.
func (h *WsHub) broadcastToClients(msg []byte) {
	for client := range h.clients {
		select {
		case client.send <- msg:
		default:
			close(client.send)
			delete(h.clients, client)
		}
	}
}

func NewWsHub(bs *gobaresip.Baresip, dataDir string, skipSipMsg string) *WsHub {
	var skipMethods []string
	if skipSipMsg != "" {
		parts := strings.Split(skipSipMsg, ",")
		for _, p := range parts {
			if m := strings.TrimSpace(p); m != "" {
				skipMethods = append(skipMethods, strings.ToUpper(m))
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &WsHub{
		clients:        make(map[*client]bool),
		command:        make(chan []byte, 128),
		register:       make(chan *client, 128),
		unregister:     make(chan *client, 128),
		broadcast:      make(chan []byte, 1024),
		bs:             bs,
		onEvent:        nil,
		onResponse:     nil,
		testStore:      nil,
		syncWaiters:    make(map[string]chan struct{}),
		ctx:            ctx,
		cancel:         cancel,
		internalCmd:    make(chan func(), 128),
		DataDir:        dataDir,
		skipSipMethods: skipMethods,
	}
}

// Stop shuts down the hub and all managed goroutines.
func (h *WsHub) Stop() {
	h.cancel()
}

// SetEventHandler sets the callback for event messages.
func (h *WsHub) SetEventHandler(handler func(gobaresip.EventMsg)) {
	h.onEvent = handler
}

// SetTestStore attaches a persistent store (SQLite) used by UI-driven testfile management.
func (h *WsHub) SetTestStore(store *TestStore) {
	h.testStore = store
}

// SetResponseHandler sets the callback for response messages.
func (h *WsHub) SetResponseHandler(handler func(gobaresip.ResponseMsg)) {
	h.onResponse = handler
}

func (h *WsHub) Run() {
	msgChan := h.bs.GetMsgChan()

	for {
		select {
		case f := <-h.internalCmd:
			f()

		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

		case msg := <-h.broadcast:
			if h.trainSession != nil {
				h.trainSession.recordRaw(msg)
			}
			h.broadcastToClients(msg)

		case <-h.ctx.Done():
			return

		case msg := <-h.command:
			input := strings.TrimSpace(string(msg))

			switch {
			case strings.HasPrefix(input, "testfiles ") || input == "testfiles":
				handleTestfilesCommand(h, input)

			case strings.HasPrefix(input, "testfile_inline ") || input == "testfile_inline":
				handleTestfileInlineCommand(h, input)

			case input == "test_stop":
				if h.testCancel != nil {
					cancelFn := h.testCancel
					h.testCancel = nil
					cancelFn()
					log.Println("test run stopped by user, cleaning up...")
					go func() {
						h.chainMu.Lock()
						defer h.chainMu.Unlock()
						executeChain(h.ctx, h, parseChain("hangupall|1s|uadelall"))
					}()
				}

			case isChained(input):
				go func(in string) {
					h.chainMu.Lock()
					defer h.chainMu.Unlock()
					executeChain(h.ctx, h, parseChain(in))
				}(input)

			default:
				cmd := expandCommand(input, &h.callIDs, h.DataDir)
				h.BroadcastCommandHint(cmd)
				if err := h.bs.CmdWs([]byte(cmd)); err != nil {
					log.Println(err)
				}
			}

		case m, ok := <-msgChan:
			if !ok {
				return
			}

			switch {
			case m.Event != nil:
				e := *m.Event
				// Call the event handler if set
				if h.onEvent != nil {
					h.onEvent(e)
				}

				// Skip RTCP stats from both training output and display
				if e.Type == "CALL_RTCP" || e.Type == "MODULE" || e.Type == "END_OF_FILE" {
					continue
				}

				// Capture Localuri -> ID mapping for call events
				if e.Class == "call" && e.Localuri != "" && e.ID != "" {
					if e.Type == "CALL_CLOSED" {
						h.callIDs.Delete(e.Localuri)
					} else {
						h.callIDs.Store(e.Localuri, e.ID)
					}
				}

				if h.trainSession != nil {
					h.trainSession.recordEvent(e)
				}

				h.broadcastToClients(e.RawJSON)

			case m.Response != nil:
				r := *m.Response
				if strings.HasPrefix(r.Token, "sync_") {
					if ch, ok := h.syncWaiters[r.Token]; ok {
						close(ch)
						delete(h.syncWaiters, r.Token)
					}
					continue
				}

				// Call the response handler if set
				if h.onResponse != nil {
					h.onResponse(r)
				}

				h.broadcastToClients(r.RawJSON)

			case m.Log != "":
				l := m.Log
				// Format as JSON and send to client
				msg, err := json.Marshal(map[string]interface{}{
					"event": true,
					"type":  "LOG",
					"param": l,
				})
				if err != nil {
					continue
				}
				if h.trainSession != nil {
					h.trainSession.recordRaw(msg)
				}
				h.broadcastToClients(msg)

			case m.SIP != "":
				s := m.SIP
				if h.shouldSkipSip(s) {
					continue
				}

				msg, err := json.Marshal(map[string]interface{}{
					"event": true,
					"type":  "SIP",
					"param": s,
				})
				if err != nil {
					continue
				}
				if h.trainSession != nil {
					h.trainSession.recordRaw(msg)
				}
				h.broadcastToClients(msg)
			}
		}
	}
}

func (h *WsHub) shouldSkipSip(msg string) bool {
	if len(h.skipSipMethods) == 0 {
		return false
	}
	lines := strings.Split(msg, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "CSeq: ") {
			// CSeq: 1234 OPTIONS
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				method := strings.ToUpper(parts[2])
				for _, skip := range h.skipSipMethods {
					if method == skip {
						return true
					}
				}
			}
			break
		}
	}
	return false
}

// BroadcastCommandHint sends a "hint" of an executed command to the Log and SIP views.
func (h *WsHub) BroadcastCommandHint(cmd string) {
	if cmd == "" {
		return
	}

	display := cmd
	if len(cmd) > 30 {
		display = cmd[:30] + "..."
	}

	count := h.cmdCounter.Add(1)
	msg, _ := json.Marshal(map[string]interface{}{
		"event":  true,
		"type":   "CMD",
		"param":  "CMD: " + display,
		"_cmdId": fmt.Sprintf("cmd_%d_%d", time.Now().Unix(), count),
	})
	h.broadcast <- msg
}
