package gobaresip

/*
#cgo linux CFLAGS: -I${SRCDIR}/../..
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/baresip/libbaresip.a
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/re/libre.a
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/rem/librem.a
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/opus/libopus.a
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/openssl/libssl.a
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/openssl/libcrypto.a
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/g722/libg722.a
#cgo linux LDFLAGS: ${SRCDIR}/../../libbaresip/sndfile/libsndfile.a
#cgo linux LDFLAGS: -ldl -lm

#include <stdint.h>
#include <stdlib.h>

#define DEBUG_MODULE "gobaresip"
#define DEBUG_LEVEL 7

#include <libbaresip/re/include/re.h>
#include <libbaresip/re/include/re_dbg.h>
#include <libbaresip/rem/include/rem.h>
#include <libbaresip/baresip/include/baresip.h>

static struct tmr tmr_quit;

static void signal_handler(int sig)
{
	static bool term = false;

	if (term) {
		module_app_unload();
		mod_close();
		exit(0);
	}

	term = true;

	info("terminated by signal %d\n", sig);

	ua_stop_all(false);
}

static void ua_exit_handler(void *arg)
{
	(void)arg;
	debug("ua exited -- stopping main runloop\n");

	//The main run-loop can be stopped now
	re_cancel();
}

static inline void set_ua_exit_handler()
{
	uag_set_exit_handler(ua_exit_handler, NULL);
}

static inline void cancelQuitTimer(){
	tmr_cancel(&tmr_quit);
}

static void my_dbg_handler(int level, const char *p, size_t len, void *arg)
{
	(void)arg;
	extern void gobaresip_log_handler(int level, char *p, int len);
	if (len > 0) {
		gobaresip_log_handler(level, (char *)p, (int)len);
	}
}

static void my_log_handler(uint32_t level, const char *msg)
{
	extern void gobaresip_log_handler(int level, char *p, int len);
	if (msg) {
		int len = strlen(msg);
		gobaresip_log_handler((int)level, (char *)msg, len);
	}
}

static struct log my_log;
static inline void register_log_handlers() {
	dbg_handler_set(my_dbg_handler, NULL);
	my_log.h = my_log_handler;
	log_register_handler(&my_log);
}

static void my_sip_trace_handler(bool tx, enum sip_transp tp,
			      const struct sa *src, const struct sa *dst,
			      const uint8_t *pkt, size_t len, void *arg)
{
	(void)arg;

	char *str = NULL;
	int err = re_sdprintf(&str,
		  "%H|%s\n"
		  "%s %J -> %J\n"
		  "%b\n"
		  ,
		  fmt_timestamp, NULL,
		  tx ? "TX" : "RX",
		  sip_transp_name(tp), src, dst, pkt, len);

	if (err == 0 && str) {
		extern void gobaresip_sip_trace_handler(char *p, int len);
		gobaresip_sip_trace_handler(str, strlen(str));
	}
	mem_deref(str);
}

static inline void enable_my_sip_trace(bool enable) {
	if (enable) {
		sip_set_trace_handler(uag_sip(), my_sip_trace_handler);
	} else {
		sip_set_trace_handler(uag_sip(), NULL);
	}
}

static inline void set_log_debug(int enable) {
	log_enable_debug(enable ? true : false);
}

static void my_ui_input(const char *str) {
	cmd_process_long(baresip_commands(), str, strlen(str), NULL, NULL);
}

static int my_ui_output_handler(const char *str) {
	extern void gobaresip_ui_output_handler(char *str);
	gobaresip_ui_output_handler((char *)str);
	return 0;
}

static struct ui my_ui = {
	.le = {NULL, NULL, NULL},
	.name = "gobaresip",
	.outputh = my_ui_output_handler,
};

static inline void register_ui_handler() {
	ui_register(baresip_uis(), &my_ui);
}

static inline int mainLoop(){
	tmr_init(&tmr_quit);
	return re_main(signal_handler);
}
*/
import "C"
import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

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
	writeMu          sync.Mutex // protects ctrlConn writes
	readWG           sync.WaitGroup
	closeOnce        sync.Once
	closeCh          chan struct{}
	remote           bool
	baresipCtrlAddr  string
	msgRecvHandler   func(Msg)
}

var activeBaresipPtr atomic.Pointer[Baresip]

func New(options ...func(*Baresip) error) (*Baresip, error) {
	b := &Baresip{
		msgChan: make(chan Msg, 10000),
		closeCh: make(chan struct{}),
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

//export gobaresip_sip_trace_handler
func gobaresip_sip_trace_handler(p *C.char, len C.int) {
	b := activeBaresipPtr.Load()
	if b != nil {
		msg := C.GoStringN(p, len)
		select {
		case b.msgChan <- Msg{SIP: msg}:
		default:
		}
	}
}

//export gobaresip_log_handler
func gobaresip_log_handler(_ C.int, p *C.char, len C.int) {
	b := activeBaresipPtr.Load()
	if b != nil {
		msg := C.GoStringN(p, len)
		select {
		case b.msgChan <- Msg{Log: msg}:
		default:
		}
	}
}

// gobaresip_bevent_handler is no longer needed as we use ctrl_tcp JSON events

//export gobaresip_ui_output_handler
func gobaresip_ui_output_handler(str *C.char) {
	b := activeBaresipPtr.Load()
	if b == nil {
		return
	}
	s := strings.TrimSpace(C.GoString(str))
	if s == "" {
		return
	}

	r := &ResponseMsg{
		Response: true,
		Ok:       true,
		Data:     s,
	}
	rawJSON, err := json.Marshal(r)
	if err != nil {
		log.Printf("gobaresip: failed to marshal UI output response: %v", err)
		return
	}
	r.RawJSON = rawJSON

	select {
	case b.msgChan <- Msg{Response: r}:
	default:
	}
}

func (b *Baresip) connectCtrl() error {
	var err error
	b.ctrlConn, err = net.Dial("tcp", b.ctrlAddr)
	if err != nil {
		atomic.StoreUint32(&b.ctrlConnAlive, 0)
		return fmt.Errorf("%w: please make sure ctrl_tcp is enabled", err)
	}

	b.ctrlStream = newReader(b.ctrlConn)

	atomic.StoreUint32(&b.ctrlConnAlive, 1)
	return nil
}

var (
	eventMarker    = []byte("\"event\":true")
	responseMarker = []byte("\"response\":true")
)

func (b *Baresip) read() {
	defer b.readWG.Done()

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

		// Single-pass dispatch: check event first, then response.
		if bytes.Contains(msg, eventMarker) {
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
			default:
				log.Println("gobaresip: msgChan full, dropping event")
			}
		} else if bytes.Contains(msg, responseMarker) {
			var r ResponseMsg
			if err := json.Unmarshal(msg, &r); err != nil {
				log.Println(err, string(msg))
				continue
			}
			r.RawJSON = msg

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
			default:
				log.Println("gobaresip: msgChan full, dropping response")
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

func (b *Baresip) Close() {
	b.closeOnce.Do(func() {
		close(b.closeCh)
		atomic.StoreUint32(&b.ctrlConnAlive, 0)
		if b.ctrlConn != nil {
			if err := b.ctrlConn.Close(); err != nil {
				log.Printf("gobaresip: close ctrl connection failed: %v", err)
			}
		}

		b.readWG.Wait()

		close(b.msgChan)
		activeBaresipPtr.Store(nil)
	})
}

// GetMsgChan returns the receive-only Msg channel for reading ordered data.
func (b *Baresip) GetMsgChan() <-chan Msg {
	return b.msgChan
}

// setup a baresip instance
func (b *Baresip) setup() error {

	ua := C.CString(b.userAgent)
	defer C.free(unsafe.Pointer(ua))

	C.sys_coredump_set(false)

	err := C.libre_init()
	if err != 0 {
		log.Printf("libre init failed with error code %d\n", err)
		return b.end(err)
	}

	C.re_thread_async_init(4)

	C.log_enable_stdout(false)

	if b.configPath != "" {
		cp := C.CString(b.configPath)
		defer C.free(unsafe.Pointer(cp))
		C.conf_path_set(cp)
	}

	err = C.conf_configure()
	if err != 0 {
		log.Printf("baresip configure failed with error code %d\n", err)
		return b.end(err)
	}

	// Top-level baresip struct init must be done AFTER configuration is complete.
	err = C.baresip_init(C.conf_config())
	if err != 0 {
		log.Printf("baresip main init failed with error code %d\n", err)
		return b.end(err)
	}

	if b.audioPath != "" {
		ap := C.CString(b.audioPath)
		defer C.free(unsafe.Pointer(ap))
		C.play_set_path(C.baresip_player(), ap)
	}

	err = C.ua_init(ua, true, true, true)
	if err != 0 {
		log.Printf("baresip ua init failed with error code %d\n", err)
		return b.end(err)
	}

	C.set_ua_exit_handler()

	err = C.conf_modules()
	if err != 0 {
		log.Printf("baresip load modules failed with error code %d\n", err)
		return b.end(err)
	}

	C.register_log_handlers()
	C.log_enable_timestamps(true)
	C.enable_my_sip_trace(true)

	if b.remote {
		if err := b.connectCtrl(); err != nil {
			b.end(1)
			return err
		}
	} else {
		// In Multi-Process Agent mode, we use CGO UI handler for logs
		// and the sip_trace_handler for SIP messages.
		// These go to b.msgChan and are bridged in startProxy.
		C.register_ui_handler()
	}

	return nil
}

// Run a baresip instance
func (b *Baresip) Run() error {
	if !b.remote {
		if err := b.startProxy(); err != nil {
			return err
		}
	}
	b.readWG.Add(1)
	go b.read()
	return b.end(C.mainLoop())
}

func (b *Baresip) end(errCode C.int) error {
	C.cancelQuitTimer()

	if errCode != 0 {
		C.ua_stop_all(true)
	}

	C.ua_close()
	C.module_app_unload()
	C.conf_close()

	C.baresip_close()

	// Modules must be unloaded after all application activity has stopped.
	C.mod_close()

	C.re_thread_async_close()

	/* Check for open timers */
	C.tmr_debug()

	C.libre_close()

	if errCode != 0 {
		return fmt.Errorf("baresip exited with error code %d", errCode)
	}
	return nil
}

// startProxy starts a TCP server that acts as a bridge between the Master
// and Baresip (CGO). It multiplexes events, logs, and SIP traces.
func (b *Baresip) startProxy() error {
	log.Printf("gobaresip: agent proxy starting to listen on %s", b.ctrlAddr)
	l, err := net.Listen("tcp", b.ctrlAddr)
	if err != nil {
		log.Printf("gobaresip: agent proxy listen failed: %v", err)
		return err
	}
	log.Printf("gobaresip: agent proxy listening on %s", b.ctrlAddr)

	go func() {
		defer func() {
			if err := l.Close(); err != nil {
				log.Printf("gobaresip: agent proxy listener close failed: %v", err)
			}
		}()
		for {
			conn, err := l.Accept()
			if err != nil {
				log.Printf("gobaresip: agent proxy accept failed: %v", err)
				return
			}
			go b.handleProxyConn(conn)
		}
	}()
	return nil
}

func (b *Baresip) handleProxyConn(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("gobaresip: proxy master connection close failed: %v", err)
		}
	}()
	log.Printf("gobaresip: proxy accepted connection from %s", conn.RemoteAddr())

	// Connect to local Baresip's ctrl_tcp (Internal)
	log.Printf("gobaresip: proxy connecting to local baresip at %s", b.baresipCtrlAddr)
	localConn, err := net.Dial("tcp", b.baresipCtrlAddr)
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
		netstring := fmt.Sprintf("%d:%s,", len(data), string(data))
		if _, err := conn.Write([]byte(netstring)); err != nil {
			log.Printf("gobaresip: proxy write to master failed: %v", err)
			return err
		}
		return nil
	}

	// 1. Forward Baresip (TCP) -> Master (Events/Responses)
	go func() {
		// We use a internal reader to read netstrings from local Baresip
		rLocal := newReader(localConn)
		for {
			msg, err := rLocal.readNetstring()
			if err != nil {
				return
			}
			// Enrichment: add agent alias to every JSON event/response
			var m map[string]interface{}
			if err := json.Unmarshal(msg, &m); err == nil {
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
	go func() {
		for {
			select {
			case m, ok := <-b.msgChan:
				if !ok {
					return
				}
				var data []byte
				switch {
				case m.SIP != "":
					data, err = json.Marshal(map[string]interface{}{
						"event":  true,
						"type":   "SIP",
						"param":  m.SIP,
						"_agent": b.alias,
					})
					if err != nil {
						log.Printf("gobaresip: proxy failed to marshal SIP message: %v", err)
						continue
					}
				case m.Log != "":
					data, err = json.Marshal(map[string]interface{}{
						"event":  true,
						"type":   "LOG",
						"param":  m.Log,
						"_agent": b.alias,
					})
					if err != nil {
						log.Printf("gobaresip: proxy failed to marshal log message: %v", err)
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
	<-b.closeCh
}

// CmdDirect executes a command directly via baresip C API (for Agent mode)
func (b *Baresip) CmdDirect(command, params, token string) error {
	fullCmd := strings.TrimSpace(command)
	if fullCmd == "" {
		return nil
	}
	if params != "" {
		fullCmd += " " + params
	}
	cCmd := C.CString(fullCmd)
	defer C.free(unsafe.Pointer(cCmd))
	C.my_ui_input(cCmd)
	return nil
}
