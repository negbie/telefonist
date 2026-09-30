package gobaresip

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goccy/go-json"
)

// ResponseMsg
type ResponseMsg struct {
	Response bool   `json:"response,omitempty"`
	Ok       bool   `json:"ok,omitempty"`
	Data     string `json:"data,omitempty"`
	Token    string `json:"token,omitempty"`
	RawJSON  []byte `json:"-"`
}

// EventMsg
type EventMsg struct {
	Event           bool   `json:"event,omitempty"`
	Class           string `json:"class,omitempty"`
	Type            string `json:"type,omitempty"`
	AccountAOR      string `json:"accountaor,omitempty"`
	Cuser           string `json:"cuser,omitempty"`
	Direction       string `json:"direction,omitempty"`
	PeerURI         string `json:"peeruri,omitempty"`
	Contacturi      string `json:"contacturi,omitempty"`
	Localuri        string `json:"localuri,omitempty"`
	PeerDisplayname string `json:"peerdisplayname,omitempty"`
	ID              string `json:"id,omitempty"`
	RemoteAudioDir  string `json:"remoteaudiodir,omitempty"`
	Audiodir        string `json:"audiodir,omitempty"`
	Localaudiodir   string `json:"localaudiodir,omitempty"`
	Param           string `json:"param,omitempty"`
	RawJSON         []byte `json:"-"`
}

// Msg holds any of the message types from baresip to ensure ordering.
type Msg struct {
	Event    *EventMsg
	Response *ResponseMsg
	Log      string
	SIP      string
}

type Baresip struct {
	userAgent        string
	alias            string
	ctrlAddr         string
	configPath       string
	audioPath        string
	ctrlConn         net.Conn
	ctrlConnAlive    uint32
	msgChan          chan Msg
	ctrlStream       *reader
	ctx              context.Context
	reconnect        bool
	reconnectBackoff time.Duration
	writeMu          sync.Mutex // protects ctrlConn writes and reconnect updates
	readWG           sync.WaitGroup
	readDone         chan struct{}
	closeOnce        sync.Once
	closeCh          chan struct{}
	msgMu            sync.RWMutex // protects msgChan sends against channel closure
	remote           bool
	baresipCtrlAddr  string
	msgRecvHandler   func(Msg)
	dropped          atomic.Uint64
	requestID        atomic.Uint64
	pendingMu        sync.Mutex
	pending          map[string]chan ResponseMsg
	nativeComplete   chan struct{}
	proxyComplete    chan struct{}
}

var activeBaresipPtr atomic.Pointer[Baresip]

func New(options ...func(*Baresip) error) (*Baresip, error) {
	b := &Baresip{
		msgChan:        make(chan Msg, 128),
		closeCh:        make(chan struct{}),
		readDone:       make(chan struct{}),
		nativeComplete: make(chan struct{}),
		proxyComplete:  make(chan struct{}, 1),
	}

	if err := b.SetOption(options...); err != nil {
		return nil, err
	}

	if b.audioPath == "" {
		b.audioPath = "."
	}
	if b.configPath == "" {
		b.configPath = "."
	}
	if b.ctrlAddr == "" {
		b.ctrlAddr = "127.0.0.1:4444"
	}
	if b.userAgent == "" {
		b.userAgent = "go-baresip"
	}
	if b.alias == "" {
		b.alias = b.userAgent
	}
	if b.ctx == nil {
		b.ctx = context.Background()
	}
	if b.reconnectBackoff == 0 {
		b.reconnectBackoff = 2 * time.Second
	}

	if b.remote {
		if err := b.connectCtrl(); err != nil {
			return nil, err
		}
		b.readWG.Add(1)
		go b.read()
		return b, nil
	}

	activeBaresipPtr.Store(b)

	if err := b.setup(); err != nil {
		activeBaresipPtr.Store(nil)
		return nil, err
	}

	return b, nil
}

func SetBaresipCtrlAddr(addr string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.baresipCtrlAddr = addr
		return nil
	}
}

func SetAlias(alias string) func(*Baresip) error {
	return func(b *Baresip) error {
		b.alias = alias
		return nil
	}
}

// sendMsg safely delivers a message to msgChan without racing with Close().
func (b *Baresip) sendMsg(m Msg) {
	b.msgMu.RLock()
	defer b.msgMu.RUnlock()
	if len(m.SIP)+len(m.Log) > maxNetstringSize {
		b.dropped.Add(1)
		return
	}
	select {
	case <-b.closeCh:
		return
	default:
		select {
		case b.msgChan <- m:
		default:
			b.dropped.Add(1)
		}
	}
}

func splitControlAddr(addr string) (network string, address string) {
	if strings.HasPrefix(addr, "unix:") {
		return "unix", strings.TrimPrefix(addr, "unix:")
	}
	return "tcp", addr
}

func listenControl(addr string) (net.Listener, error) {
	network, address := splitControlAddr(addr)
	if network == "unix" {
		if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return net.Listen(network, address)
}

func dialControl(addr string) (net.Conn, error) {
	network, address := splitControlAddr(addr)
	return net.DialTimeout(network, address, 2*time.Second)
}

func (b *Baresip) connectCtrl() error {
	conn, err := dialControl(b.ctrlAddr)
	if err != nil {
		atomic.StoreUint32(&b.ctrlConnAlive, 0)
		return fmt.Errorf("%w: please make sure ctrl_tcp is enabled", err)
	}

	r := newReader(conn)

	b.writeMu.Lock()
	if b.ctrlConn != nil {
		_ = b.ctrlConn.Close()
	}
	b.ctrlConn = conn
	b.ctrlStream = r
	atomic.StoreUint32(&b.ctrlConnAlive, 1)
	b.writeMu.Unlock()
	return nil
}

func (b *Baresip) read() {
	defer b.readWG.Done()
	defer close(b.readDone)

	for {
		if atomic.LoadUint32(&b.ctrlConnAlive) == 0 {
			if !b.tryReconnect() {
				break
			}
			continue
		}

		msg, err := b.ctrlStream.readNetstring()
		if err != nil {
			log.Println(err)
			atomic.StoreUint32(&b.ctrlConnAlive, 0)
			if !b.tryReconnect() {
				break
			}
			continue
		}

		var envelope struct {
			Event    bool `json:"event"`
			Response bool `json:"response"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			raw := []byte(`{"event":true,"type":"TELEMETRY_LOSS","param":"malformed control JSON"}`)
			select {
			case b.msgChan <- Msg{Event: &EventMsg{Type: "TELEMETRY_LOSS", RawJSON: raw}}:
			case <-b.closeCh:
			case <-b.ctx.Done():
			}
			return
		}
		if envelope.Event {
			var e EventMsg
			e.RawJSON = msg

			if err := json.Unmarshal(e.RawJSON, &e); err != nil {
				log.Println(err, string(e.RawJSON))
				continue
			}

			if b.msgRecvHandler != nil {
				b.msgRecvHandler(Msg{Event: &e})
				continue
			}

			select {
			case b.msgChan <- Msg{Event: &e}:
			case <-b.closeCh:
				return
			case <-b.ctx.Done():
				return
			}
		} else if envelope.Response {
			var r ResponseMsg
			if err := json.Unmarshal(msg, &r); err != nil {
				log.Println(err, string(msg))
				continue
			}
			r.RawJSON = msg
			b.pendingMu.Lock()
			if reply := b.pending[r.Token]; reply != nil {
				reply <- r
				delete(b.pending, r.Token)
			}
			b.pendingMu.Unlock()

			if b.msgRecvHandler != nil {
				b.msgRecvHandler(Msg{Response: &r})
				continue
			}

			select {
			case b.msgChan <- Msg{Response: &r}:
			case <-b.closeCh:
				return
			case <-b.ctx.Done():
				return
			}
		}
	}
}

// tryReconnect attempts to re-establish the ctrl_tcp connection with
// exponential backoff. Returns false if reconnect is disabled or the
// context / closeCh signals shutdown.
func (b *Baresip) tryReconnect() bool {
	if !b.reconnect {
		return false
	}

	backoff := b.reconnectBackoff
	const maxBackoff = 30 * time.Second

	for attempt := 0; ; attempt++ {
		select {
		case <-b.closeCh:
			return false
		case <-b.ctx.Done():
			return false
		default:
		}

		wait := time.Duration(float64(backoff) * math.Pow(1.5, float64(attempt)))
		if wait > maxBackoff {
			wait = maxBackoff
		}

		log.Printf("gobaresip: reconnecting in %v (attempt %d)\n", wait, attempt+1)

		select {
		case <-time.After(wait):
		case <-b.closeCh:
			return false
		case <-b.ctx.Done():
			return false
		}

		if err := b.connectCtrl(); err != nil {
			log.Printf("gobaresip: reconnect failed: %v\n", err)
			continue
		}

		log.Println("gobaresip: reconnected successfully")
		return true
	}
}

func (b *Baresip) WaitReadDrain(ctx context.Context) error {
	select {
	case <-b.readDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Baresip) Close() {
	b.closeOnce.Do(func() {
		close(b.closeCh)
		atomic.StoreUint32(&b.ctrlConnAlive, 0)

		b.writeMu.Lock()
		conn := b.ctrlConn
		b.ctrlConn = nil
		b.writeMu.Unlock()

		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Printf("gobaresip: close ctrl connection failed: %v", err)
			}
		}

		b.readWG.Wait()

		activeBaresipPtr.Store(nil)
		b.msgMu.Lock()
		close(b.msgChan)
		b.msgMu.Unlock()
	})
}

// GetMsgChan returns the receive-only Msg channel for reading ordered data.
func (b *Baresip) GetMsgChan() <-chan Msg {
	return b.msgChan
}

// startProxy starts a control server that acts as a bridge between the Master
// and Baresip (CGO). It multiplexes events, logs, and SIP traces.
func (b *Baresip) startProxy() error {
	log.Printf("gobaresip: agent proxy starting to listen on %s", b.ctrlAddr)
	l, err := listenControl(b.ctrlAddr)
	if err != nil {
		log.Printf("gobaresip: agent proxy listen failed: %v", err)
		return err
	}
	log.Printf("gobaresip: agent proxy listening on %s", b.ctrlAddr)
	go func() {
		select {
		case <-b.closeCh:
		case <-b.ctx.Done():
		}
		_ = l.Close()
	}()

	go func() {
		defer func() {
			if err := l.Close(); err != nil {
				log.Printf("gobaresip: agent proxy listener close failed: %v", err)
			}
			if network, address := splitControlAddr(b.ctrlAddr); network == "unix" {
				if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
					log.Printf("gobaresip: agent proxy socket cleanup failed: %v", err)
				}
			}
		}()
		for {
			conn, err := l.Accept()
			if err != nil {
				log.Printf("gobaresip: agent proxy accept failed: %v", err)
				return
			}
			// One master owns the telemetry stream; never split it across readers.
			b.handleProxyConn(conn)
		}
	}()
	return nil
}

func (b *Baresip) handleProxyConn(conn net.Conn) {
	defer func() {
		select {
		case b.proxyComplete <- struct{}{}:
		default:
		}
	}()
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done); _ = conn.Close() }) }
	defer stop()
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("gobaresip: proxy master connection close failed: %v", err)
		}
	}()
	log.Printf("gobaresip: proxy accepted connection from %s", conn.RemoteAddr())

	// Connect to local Baresip's ctrl_tcp (Internal)
	log.Printf("gobaresip: proxy connecting to local baresip at %s", b.baresipCtrlAddr)
	localConn, err := net.DialTimeout("tcp", b.baresipCtrlAddr, 2*time.Second)
	if err != nil {
		log.Printf("gobaresip: proxy failed to connect to local baresip: %v", err)
		return
	}
	defer func() {
		if err := localConn.Close(); err != nil {
			log.Printf("gobaresip: proxy local connection close failed: %v", err)
		}
	}()

	// Use a mutex to protect concurrent writes to the Master Hub connection
	var writeMu sync.Mutex
	writeToMaster := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		netstring := fmt.Sprintf("%d:%s,", len(data), string(data))
		if _, err := conn.Write([]byte(netstring)); err != nil {
			log.Printf("gobaresip: proxy write to master failed: %v", err)
			return err
		}
		return nil
	}

	// 1. Forward Baresip (TCP) -> Master (Events/Responses)
	localDone := make(chan struct{})
	go func() {
		defer close(localDone)
		// We use a internal reader to read netstrings from local Baresip
		rLocal := newReader(localConn)
		for {
			msg, err := rLocal.readNetstring()
			if err != nil {
				return
			}
			// Enrichment: add agent alias to every JSON event/response
			var m map[string]interface{}
			if err := json.Unmarshal(msg, &m); err == nil && m != nil {
				m["_agent"] = b.alias
				encoded, err := json.Marshal(m)
				if err != nil {
					log.Printf("gobaresip: proxy failed to marshal enriched message: %v", err)
				} else {
					msg = encoded
				}
			}

			if err := writeToMaster(msg); err != nil {
				return
			}
		}
	}()

	// 2. Forward msgChan (CGO) -> Master (SIP Traces/Logs)
	sendCaptured := func(m Msg) error {
		kind, param := "LOG", m.Log
		if m.SIP != "" {
			kind, param = "SIP", m.SIP
		}
		data, e := json.Marshal(map[string]any{"event": true, "type": kind, "param": param, "_agent": b.alias})
		if e != nil {
			return e
		}
		return writeToMaster(data)
	}
	reportLoss := func() error {
		if count := b.dropped.Swap(0); count > 0 {
			data, _ := json.Marshal(map[string]any{"event": true, "type": "TELEMETRY_LOSS", "_agent": b.alias, "param": fmt.Sprintf("%d native messages lost", count)})
			return writeToMaster(data)
		}
		return nil
	}
	go func() {
		defer stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-b.nativeComplete:
				select {
				case <-localDone:
				case <-done:
					return
				}
				for {
					select {
					case m := <-b.msgChan:
						if sendCaptured(m) != nil {
							return
						}
					default:
						if reportLoss() != nil {
							return
						}
						_ = writeToMaster([]byte(`{"event":true,"type":"NATIVE_DONE"}`))
						return
					}
				}
			case <-done:
				return
			case <-ticker.C:
				if count := b.dropped.Swap(0); count > 0 {
					data, _ := json.Marshal(map[string]any{"event": true, "type": "TELEMETRY_LOSS", "_agent": b.alias, "param": fmt.Sprintf("%d native messages lost", count)})
					if writeToMaster(data) != nil {
						return
					}
				}
			case m, ok := <-b.msgChan:
				if !ok {
					return
				}
				var data []byte
				switch {
				case m.SIP != "":
					var marshalErr error
					data, marshalErr = json.Marshal(map[string]interface{}{
						"event":  true,
						"type":   "SIP",
						"param":  m.SIP,
						"_agent": b.alias,
					})
					if marshalErr != nil {
						log.Printf("gobaresip: proxy failed to marshal SIP message: %v", marshalErr)
						continue
					}
				case m.Log != "":
					var marshalErr error
					data, marshalErr = json.Marshal(map[string]interface{}{
						"event":  true,
						"type":   "LOG",
						"param":  m.Log,
						"_agent": b.alias,
					})
					if marshalErr != nil {
						log.Printf("gobaresip: proxy failed to marshal log message: %v", marshalErr)
						continue
					}
				}
				if len(data) > 0 {
					if err := writeToMaster(data); err != nil {
						return
					}
				}
			case <-b.closeCh:
				return
			}
		}
	}()

	// 3. Forward Master -> Baresip (Commands)
	go func() {
		defer stop()
		rMaster := newReader(conn)
		for {
			msg, err := rMaster.readNetstring()
			if err != nil {
				log.Printf("gobaresip: proxy read from master failed: %v", err)
				return
			}
			// Forward exactly as received (netstring wrap)
			netstring := fmt.Sprintf("%d:%s,", len(msg), string(msg))
			if _, err := localConn.Write([]byte(netstring)); err != nil {
				log.Printf("gobaresip: proxy write to local baresip failed: %v", err)
				return
			}
		}
	}()

	// Wait for connection to close or agent to stop
	select {
	case <-b.closeCh:
	case <-b.ctx.Done():
	case <-done:
	}
}
