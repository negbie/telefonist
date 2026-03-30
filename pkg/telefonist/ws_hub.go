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

	bm       *BaresipManager
	agentMsg chan AgentMsg

	// activeAgent is the current target for commands (set by uafind)
	activeAgent string

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

	// chainMu ensures only one executeChain runs at a time,
	// preventing interleaving of multi-step command sequences.
	chainMu sync.Mutex

	DataDir string

	cmdCounter atomic.Uint64

	// Message history for persistence across refreshes
	history      [][]byte
	historyLimit int
}

type AgentMsg struct {
	Alias    string
	Msg      gobaresip.Msg
	Sentinel chan struct{}
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

// recordAndBroadcast adds a message to the hub's recent history and then
// broadcasts it to all connected clients. History is replayed to new clients.
func (h *WsHub) recordAndBroadcast(msg []byte) {
	if h.historyLimit <= 0 {
		h.broadcastToClients(msg)
		return
	}
	if len(h.history) < h.historyLimit {
		h.history = append(h.history, msg)
	} else {
		// Overwrite the oldest message with the latest to keep O(1) memory.
		// copy handles the shift, aiding GC by dropping the oldest reference.
		copy(h.history, h.history[1:])
		h.history[h.historyLimit-1] = msg
	}
	h.broadcastToClients(msg)
}

func NewWsHub(dataDir string, maxCalls uint, rtpNet string, rtpPorts string, rtpTimeout uint, useAlsa bool, sipListen string) *WsHub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &WsHub{
		clients:      make(map[*client]bool),
		command:      make(chan []byte, 128),
		register:     make(chan *client, 128),
		unregister:   make(chan *client, 128),
		broadcast:    make(chan []byte, 1024),
		bm:           nil, // Set later
		agentMsg:     make(chan AgentMsg, 1024),
		onEvent:      nil,
		onResponse:   nil,
		testStore:    nil,
		syncWaiters:  make(map[string]chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		internalCmd:  make(chan func(), 128),
		DataDir:      dataDir,
		history:      make([][]byte, 0, 3333),
		historyLimit: 3333,
	}
	h.bm = NewBaresipManager(h, dataDir, maxCalls, rtpNet, rtpPorts, rtpTimeout, useAlsa, sipListen)
	return h
}

func (h *WsHub) ForwardAgentMsg(alias string, msg gobaresip.Msg) {
	select {
	case h.agentMsg <- AgentMsg{Alias: alias, Msg: msg}:
	default:
		log.Printf("hub: agentMsg chan full, dropping msg from %s", alias)
	}
}

// Stop shuts down the hub and all managed goroutines.
func (h *WsHub) Stop() {
	h.cancel()
}

// Drain blocks until all currently queued agent messages are processed by Run.
func (h *WsHub) Drain() {
	done := make(chan struct{})
	select {
	case h.agentMsg <- AgentMsg{Sentinel: done}:
		<-done
	case <-time.After(3 * time.Second):
		log.Println("hub: drain timeout")
	}
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
	for {
		select {
		case f := <-h.internalCmd:
			f()

		case client := <-h.register:
			// Replay history to the new client first
			for _, msg := range h.history {
				select {
				case client.send <- msg:
				default:
					// drop it if buffer is full; should not happen with increased buffer
				}
			}
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
			h.recordAndBroadcast(msg)

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
				cmd := expandCommand(input, h.DataDir)
				h.executeSmartCommand(cmd)
			}

		case am, ok := <-h.agentMsg:
			if !ok {
				return
			}
			if am.Sentinel != nil {
				close(am.Sentinel)
				continue
			}
			m := am.Msg

			switch {
			case m.Event != nil:
				e := *m.Event
				if h.onEvent != nil {
					h.onEvent(e)
				}

				if e.Type == "CALL_RTCP" || e.Type == "MODULE" || e.Type == "END_OF_FILE" {
					continue
				}

				// No automatic ringing state tracking needed anymore.

				if h.trainSession != nil {
					h.trainSession.recordEvent(e)
					if h.trainSession.failMsg != "" && h.testCancel != nil {
						h.testCancel()
					}
				}

				var mEvent map[string]interface{}
				json.Unmarshal(e.RawJSON, &mEvent)
				mEvent["_agent"] = am.Alias
				enriched, _ := json.Marshal(mEvent)
				h.recordAndBroadcast(enriched)

			case m.Response != nil:
				r := *m.Response
				if strings.HasPrefix(r.Token, "sync_") {
					if ch, ok := h.syncWaiters[r.Token]; ok {
						close(ch)
						delete(h.syncWaiters, r.Token)
					}
					continue
				}

				if h.onResponse != nil {
					h.onResponse(r)
				}

				var mResp map[string]interface{}
				json.Unmarshal(r.RawJSON, &mResp)
				mResp["_agent"] = am.Alias
				mResp["event"] = true
				mResp["type"] = "RESPONSE"
				mResp["param"] = r.Data
				enriched, _ := json.Marshal(mResp)
				h.recordAndBroadcast(enriched)

			case m.Log != "":
				l := m.Log
				msg, err := json.Marshal(map[string]interface{}{
					"event":  true,
					"type":   "LOG",
					"param":  l,
					"_agent": am.Alias,
				})
				if err != nil {
					continue
				}
				if h.trainSession != nil {
					h.trainSession.recordRaw(msg)
				}
				h.recordAndBroadcast(msg)

			case m.SIP != "":
				s := m.SIP

				msg, err := json.Marshal(map[string]interface{}{
					"event":  true,
					"type":   "SIP",
					"param":  s,
					"_agent": am.Alias,
				})
				if err != nil {
					continue
				}
				if h.trainSession != nil {
					h.trainSession.recordRaw(msg)
				}
				h.recordAndBroadcast(msg)
			}
		}
	}
}

func (h *WsHub) executeSmartCommand(cmd string) {
	h.BroadcastCommandHint(cmd)

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	target := h.activeAgent
	first := ""
	if len(parts) > 0 {
		first = strings.ToLower(parts[0])

		// Robust prefix detection: look for agentAlias:cmd
		// Since agentAlias can be a SIP URI (sip:user@host), we look for ALL colons in the first word.
		h.bm.mu.RLock()
		var bestAlias string
		var bestColonIdx int
		for a := range h.bm.agents {
			for i := len(parts[0]) - 1; i >= 0; i-- {
				if parts[0][i] == ':' {
					prefix := parts[0][:i]
					// Normalize prefix: strip SIP parameters (part after ;)
					if semi := strings.Index(prefix, ";"); semi != -1 {
						prefix = prefix[:semi]
					}
					if strings.EqualFold(a, prefix) {
						if len(a) > len(bestAlias) {
							bestAlias = a
							bestColonIdx = i
						}
					}
				}
			}
		}
		h.bm.mu.RUnlock()

		if bestAlias != "" {
			target = bestAlias
			// The command part starts after the colon of bestAlias
			cmd = parts[0][bestColonIdx+1:] + " " + strings.Join(parts[1:], " ")
			cmd = strings.TrimSpace(cmd)
			parts = strings.Fields(cmd)
			if len(parts) > 0 {
				first = strings.ToLower(parts[0])
			}
		}
	}

	// Handle orchestration commands
	if first == "uafind" && len(parts) >= 2 {
		h.activeAgent = parts[1]
		return
	}

	if first == "uadelall" {
		h.bm.CloseAll()
		h.activeAgent = ""
		return
	}

	if first == "uanew" && len(parts) >= 2 {
		accountLine := strings.Join(parts[1:], " ")
		// Try to extract alias from <alias;... or <sip:alias@...
		alias := ""
		if start := strings.Index(accountLine, "<"); start != -1 {
			if end := strings.Index(accountLine, ">"); end != -1 && end > start {
				aor := accountLine[start+1 : end]
				if semi := strings.Index(aor, ";"); semi != -1 {
					alias = aor[:semi]
				} else {
					alias = aor
				}
			}
		}

		if alias != "" {
			// Stop existing agent if it already exists to ensure fresh config for uanew
			if _, ok := h.bm.GetAgent(alias); ok {
				log.Printf("hub: agent %s already exists, stopping it first for uanew", alias)
				h.bm.StopAgent(alias)
			}
			log.Printf("hub: hatching new agent %s", alias)
			if err := h.bm.SpawnAgent(h.ctx, alias, accountLine); err != nil {
				log.Printf("hub: failed to spawn agent %s: %v", alias, err)
			} else {
				h.activeAgent = alias
			}
		}
		return
	}

	// Direct Routing: All orchestration relies on explicit agent targeting (activeAgent or prefix).

	if a, ok := h.bm.GetAgent(target); ok {
		if err := a.Baresip.CmdWs([]byte(cmd)); err != nil {
			log.Printf("hub: error sending command to agent %s: %v", target, err)
		}
	} else {
		log.Printf("hub: no active agent for command %q", cmd)
	}
	h.BroadcastCommandHint(cmd)
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
		"token":  "test",
		"_cmdId": fmt.Sprintf("cmd_%d_%d", time.Now().Unix(), count),
	})
	h.broadcast <- msg
}
