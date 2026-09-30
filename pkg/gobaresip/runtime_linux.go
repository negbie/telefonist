//go:build linux && cgo

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
#define DEBUG_LEVEL 4

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
	if (level <= DEBUG_LEVEL && len > 0) {
		gobaresip_log_handler(level, (char *)p, (int)len);
	}
}

static void my_log_handler(uint32_t level, const char *msg)
{
	extern void gobaresip_log_handler(int level, char *p, int len);
	if (level <= DEBUG_LEVEL && msg) {
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
	"fmt"
	"github.com/goccy/go-json"
	"log"
	"strings"
	"time"
	"unsafe"
)

//export gobaresip_sip_trace_handler
func gobaresip_sip_trace_handler(p *C.char, len C.int) {
	b := activeBaresipPtr.Load()
	if b != nil {
		msg := C.GoStringN(p, len)
		b.sendMsg(Msg{SIP: msg})
	}
}

//export gobaresip_log_handler
func gobaresip_log_handler(_ C.int, p *C.char, len C.int) {
	b := activeBaresipPtr.Load()
	if b != nil {
		msg := C.GoStringN(p, len)
		b.sendMsg(Msg{Log: msg})
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

	b.sendMsg(Msg{Response: r})
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

	C.re_thread_async_init(2)

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
	err := b.end(C.mainLoop())
	close(b.nativeComplete)
	select {
	case <-b.proxyComplete:
	case <-time.After(3 * time.Second):
		if err == nil {
			err = fmt.Errorf("native telemetry did not finish flushing")
		}
	}
	b.Close()
	return err
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
