package telefonist

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

var (
	// wavPattern matches the shortcut ;wav=NAME/
	wavPattern = regexp.MustCompile(`;wav=([^/]+)/`)
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

	ctx    context.Context
	cancel context.CancelFunc

	internalCmd chan func()

	// chainMu ensures only one executeChain runs at a time,
	// preventing interleaving of multi-step command sequences.
	chainMu sync.Mutex

	DataDir string

	cmdCounter atomic.Uint64

	// Message history for persistence across refreshes
	history *RingBuffer
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
	h.history.Add(msg)
	h.broadcastToClients(msg)
}

func NewWsHub(dataDir string, maxCalls uint, rtpNet string, rtpPorts string, rtpTimeout uint, useAlsa bool, sipListen string) *WsHub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &WsHub{
		clients:     make(map[*client]bool),
		command:     make(chan []byte, 128),
		register:    make(chan *client, 128),
		unregister:  make(chan *client, 128),
		broadcast:   make(chan []byte, 1024),
		bm:          nil, // Set later
		agentMsg:    make(chan AgentMsg, 1024),
		onEvent:     nil,
		onResponse:  nil,
		testStore:   nil,
		ctx:         ctx,
		cancel:      cancel,
		internalCmd: make(chan func(), 128),
		DataDir:     dataDir,
		history:     NewRingBuffer(3333),
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
			for _, msg := range h.history.GetAll() {
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
				h.executeSmartCommand(input)
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
				mEvent["time"] = time.Now().Format("2.1.2006 15:04:05.000")
				enriched, _ := json.Marshal(mEvent)
				h.recordAndBroadcast(enriched)

			case m.Response != nil:
				r := *m.Response

				if h.onResponse != nil {
					h.onResponse(r)
				}

				var mResp map[string]interface{}
				json.Unmarshal(r.RawJSON, &mResp)
				mResp["_agent"] = am.Alias
				mResp["event"] = true
				mResp["type"] = "RESPONSE"
				mResp["param"] = r.Data
				mResp["time"] = time.Now().Format("2.1.2006 15:04:05.000")
				enriched, _ := json.Marshal(mResp)
				h.recordAndBroadcast(enriched)

			case m.Log != "":
				mLog := map[string]interface{}{
					"event":  true,
					"type":   "LOG",
					"param":  m.Log,
					"_agent": am.Alias,
					"time":   time.Now().Format("2.1.2006 15:04:05.000"),
				}
				msg, _ := json.Marshal(mLog)
				if h.trainSession != nil {
					h.trainSession.recordRaw(msg)
				}
				h.recordAndBroadcast(msg)

			case m.SIP != "":
				mSIP := map[string]interface{}{
					"event":  true,
					"type":   "SIP",
					"param":  m.SIP,
					"_agent": am.Alias,
					"time":   time.Now().Format("2.1.2006 15:04:05.000"),
				}
				msg, _ := json.Marshal(mSIP)
				if h.trainSession != nil {
					h.trainSession.recordRaw(msg)
				}
				h.recordAndBroadcast(msg)
			}
		}
	}
}

func (h *WsHub) executeSmartCommand(cmd string) {
	// 1. Expand shortcuts
	soundsDir := filepath.Join(h.DataDir, "sounds")
	cmd = wavPattern.ReplaceAllString(cmd, ";audio_source=aufile,"+soundsDir+"/$1;audio_player=aufile,{{RECORDS_DIR}}/")
	cmd = strings.ReplaceAll(cmd, ";input_wav=", ";audio_source=aufile,"+soundsDir+"/")

	target, cleanedCmd := h.bm.ResolveTarget(cmd, h.activeAgent)
	cmd = cleanedCmd

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}
	first := strings.ToLower(parts[0])

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
			h.BroadcastCommandHint(cmd, alias)
		}
		return
	}

	// Direct Routing: All orchestration relies on explicit agent targeting (activeAgent or prefix).

	if a, ok := h.bm.GetAgent(target); ok {
		if strings.Contains(cmd, "{{RECORDS_DIR}}") {
			cmd = strings.ReplaceAll(cmd, "{{RECORDS_DIR}}", a.RecordingsDir)
		}
		if err := a.Baresip.CmdWs([]byte(cmd)); err != nil {
			log.Printf("hub: error sending command to agent %s: %v", target, err)
		}
	} else {
		log.Printf("hub: no active agent for command %q", cmd)
	}
	h.BroadcastCommandHint(cmd, target)
}

// BroadcastCommandHint sends a "hint" of an executed command to the Log and SIP views.
func (h *WsHub) BroadcastCommandHint(cmd string, agent string) {
	if cmd == "" {
		return
	}

	display := cmd
	if len(cmd) > 30 {
		display = cmd[:30] + "..."
	}

	count := h.cmdCounter.Add(1)
	mCmd := map[string]interface{}{
		"event":  true,
		"type":   "CMD",
		"param":  "CMD: " + display,
		"token":  "test",
		"_agent": agent,
		"_cmdId": fmt.Sprintf("cmd_%d_%d", time.Now().Unix(), count),
		"time":   time.Now().Format("2.1.2006 15:04:05.000"),
	}
	msg, _ := json.Marshal(mCmd)
	h.broadcast <- msg
}
