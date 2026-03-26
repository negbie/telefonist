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
	if b.ctx == nil {
		b.ctx = context.Background()
	}
	if b.reconnectBackoff == 0 {
		b.reconnectBackoff = 2 * time.Second
	}

	activeBaresipPtr.Store(b)

	if err := b.setup(); err != nil {
		activeBaresipPtr.Store(nil)
		return nil, err
	}

	return b, nil
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

func (b *Baresip) connectCtrl() error {
	var err error
	b.ctrlConn, err = net.Dial("tcp", b.ctrlAddr)
	if err != nil {
		atomic.StoreUint32(&b.ctrlConnAlive, 0)
		return fmt.Errorf("%v: please make sure ctrl_tcp is enabled", err)
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
				log.Println(err, string(msg)) // Added logging for unmarshal error
				continue
			}
			r.RawJSON = msg

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
			b.ctrlConn.Close()
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

	//C.set_log_debug(1)
	//C.uag_enable_sip_trace(true)
	C.register_log_handlers()
	C.log_enable_timestamps(true)
	C.enable_my_sip_trace(true)

	/*
		ua_eprm := C.CString("")
		defer C.free(unsafe.Pointer(ua_eprm))
		err = C.uag_set_extra_params(ua_eprm)
	*/

	if err := b.connectCtrl(); err != nil {
		b.end(1)
		return err
	}

	return nil
}

// Run a baresip instance
func (b *Baresip) Run() error {
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

	// Check for memory leaks
	C.mem_debug()

	if errCode != 0 {
		return fmt.Errorf("baresip exited with error code %d", errCode)
	}
	return nil
}
