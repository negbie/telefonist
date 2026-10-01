package telefonist

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

var (
	// wavPattern matches both ;wav=NAME/ and wav=NAME/ (case-insensitive, optionally with leading whitespace)
	wavPattern = regexp.MustCompile(`(?i)(?:^|[\s;])wav=([^/\s]+)/`)
	// inputWavPattern matches both ;input_wav=NAME and input_wav=NAME (case-insensitive, optionally with leading whitespace)
	inputWavPattern = regexp.MustCompile(`(?i)(?:^|[\s;])input_wav=`)
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
	activeAgent atomic.Pointer[string]

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
	cancelMu   sync.Mutex
	testCancel context.CancelFunc

	ctx    context.Context
	cancel context.CancelFunc

	internalCmd chan func()

	// chainMu ensures only one executeChain runs at a time,
	// preventing interleaving of multi-step command sequences.
	chainMu sync.Mutex

	// logFilters are case-insensitive substrings used to suppress LOG messages
	// from live output and test recordings.
	logFilterMu sync.RWMutex
	logFilters  []string

	DataDir string

	cmdCounter atomic.Uint64

	// Message history for persistence across refreshes
	history *RingBuffer

	// CronManager reference for REST API
	cronManager *CronManager
	runner      *Runner
	sequence    uint64
	epoch       string
	done        chan struct{}
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
	var value map[string]any
	if json.Unmarshal(msg, &value) == nil {
		h.sequence++
		value["_seq"] = h.sequence
		if h.epoch == "" {
			h.epoch = fmt.Sprint(time.Now().UnixNano())
		}
		value["_epoch"] = h.epoch
		msg, _ = json.Marshal(value)
	}
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
		done:        make(chan struct{}),
	}
	h.setActiveAgent("")
	h.bm = NewBaresipManager(h, dataDir, maxCalls, rtpNet, rtpPorts, rtpTimeout, useAlsa, sipListen)
	h.runner = newRunner(h)
	return h
}

func (h *WsHub) getActiveAgent() string {
	p := h.activeAgent.Load()
	if p == nil {
		return ""
	}
	return *p
}

func (h *WsHub) setActiveAgent(agent string) {
	h.activeAgent.Store(&agent)
}

func (h *WsHub) setTestCancel(cancel context.CancelFunc) {
	h.cancelMu.Lock()
	h.testCancel = cancel
	h.cancelMu.Unlock()
}

func (h *WsHub) cancelTest() {
	h.cancelMu.Lock()
	cancel := h.testCancel
	h.testCancel = nil
	h.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}


func (h *WsHub) ForwardAgentMsg(alias string, msg gobaresip.Msg) {
	select {
	case h.agentMsg <- AgentMsg{Alias: alias, Msg: msg}:
	case <-h.ctx.Done():
	}
}

// Stop shuts down the hub and all managed goroutines.
func (h *WsHub) Stop() {
	h.runner.Close()
	h.cancel()
}

// Drain blocks until all currently queued agent messages are processed by Run.
func (h *WsHub) Drain() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = h.DrainContext(ctx)
}

func (h *WsHub) DrainContext(ctx context.Context) error {
	done := make(chan struct{})
	select {
	case h.agentMsg <- AgentMsg{Sentinel: done}:
	case <-ctx.Done():
		return ctx.Err()
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}

func (h *WsHub) Do(ctx context.Context, f func()) error {
	done := make(chan struct{})
	var execution sync.Mutex
	abandoned := false
	select {
	case h.internalCmd <- func() {
		execution.Lock()
		defer execution.Unlock()
		defer close(done)
		if !abandoned && ctx.Err() == nil && h.ctx.Err() == nil {
			f()
		}
	}:
	case <-ctx.Done():
		return ctx.Err()
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
	select {
	case <-done:
		return ctx.Err()
	case <-ctx.Done():
		execution.Lock()
		abandoned = true
		execution.Unlock()
		return ctx.Err()
	case <-h.ctx.Done():
		execution.Lock()
		abandoned = true
		execution.Unlock()
		return h.ctx.Err()
	}
}

// SetEventHandler sets the callback for event messages.
func (h *WsHub) SetEventHandler(handler func(gobaresip.EventMsg)) {
	h.onEvent = handler
}

// SetTestStore attaches a persistent store (SQLite) used by UI-driven testfile management.
func (h *WsHub) SetTestStore(store *TestStore) {
	h.testStore = store
	if store == nil {
		return
	}
	filters, err := store.GetLogFilters(context.Background())
	if err != nil {
		log.Printf("log filter: failed to load settings: %v", err)
		return
	}
	h.SetLogFilters(filters)
}

// SetCronManager sets the cron job scaler engine
func (h *WsHub) SetCronManager(cm *CronManager) {
	h.cronManager = cm
}

// SetResponseHandler sets the callback for response messages.
func (h *WsHub) SetResponseHandler(handler func(gobaresip.ResponseMsg)) {
	h.onResponse = handler
}

func (h *WsHub) Run() {
	defer close(h.done)
	go h.runner.manualLoop()
	defer func() {
		for c := range h.clients {
			close(c.send)
			delete(h.clients, c)
		}
	}()
	for {
		select {
		case f := <-h.internalCmd:
			f()

		case client := <-h.register:
			// Replay history to the new client first. Batching preserves the
			// newline-delimited message format while reducing channel/frame overhead.
			h.history.ReplayBatchesTo(client.send, 250, 512*1024)
			h.clients[client] = true
			state, _ := json.Marshal(map[string]any{"token": "runner", "state": h.runner.Snapshot()})
			client.send <- state

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}

		case msg := <-h.broadcast:
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
				h.runner.Stop()
			default:
				h.runner.Manual(input)
			}

		case am, ok := <-h.agentMsg:
			if !ok {
				return
			}
			if am.Sentinel != nil {
				close(am.Sentinel)
				continue
			}
			h.handleAgentMsg(am)
		}
	}
}

func ExpandShortcuts(cmd string, soundsDir string, recordsDir string) string {
	if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(cmd)), "uanew ") {
		return cmd
	}
	if recordsDir != "" && !strings.HasSuffix(recordsDir, "/") {
		recordsDir += "/"
	}

	cmd = inputWavPattern.ReplaceAllString(cmd, ";audio_source=aufile,"+soundsDir+"/")
	cmd = wavPattern.ReplaceAllString(cmd, ";audio_source=aufile,"+soundsDir+"/$1;audio_player=aufile,"+recordsDir)

	return cmd
}

// BroadcastCommandHint sends a "hint" of an executed command to the Log and SIP views.
func (h *WsHub) BroadcastCommandHint(cmd string, agent string) {
	if cmd == "" {
		return
	}

	cmd = SanitizeString(cmd)
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
		"time":   FormatNow(),
	}
	msg, _ := json.Marshal(mCmd)
	select {
	case h.broadcast <- msg:
	case <-h.ctx.Done():
	}
}
