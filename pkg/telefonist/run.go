package telefonist

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

func RunAgent(f AppFlags) error {
	if err := os.MkdirAll(f.DataDir, 0755); err != nil {
		return err
	}

	gb, err := newBaresipInstance(f)
	if err != nil {
		return err
	}

	return gb.Run()
}

func newBaresipInstance(f AppFlags) (*gobaresip.Baresip, error) {
	ua := fmt.Sprintf("telefonist/%s (baresip)", Version)
	return gobaresip.New(
		gobaresip.SetAudioPath("sounds"),
		gobaresip.SetConfigPath(f.DataDir),
		gobaresip.SetAlias(f.Alias),
		gobaresip.SetUserAgent(ua),
		gobaresip.SetCtrlTCPAddr(f.CtrlAddr),
		gobaresip.SetBaresipCtrlAddr(f.BaresipCtrlAddr),
	)
}


func startHTTPServer(f AppFlags, hub *WsHub) {
	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws", AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))

	// Auth handlers
	mux.HandleFunc("/api/login", HandleLogin(f.UiAdminPassword))
	mux.HandleFunc("/api/logout", HandleLogout)

	// REST API handlers for testfile and project management
	mux.HandleFunc("/api/projects", AuthMiddleware(HandleAPIProjects(hub)))
	mux.HandleFunc("/api/testfiles", AuthMiddleware(HandleAPITestfiles(hub)))
	mux.HandleFunc("/api/testfile", AuthMiddleware(HandleAPITestfile(hub)))
	mux.HandleFunc("/api/testfile/rename", AuthMiddleware(HandleAPITestfileRename(hub)))
	mux.HandleFunc("/api/testruns", AuthMiddleware(HandleAPITestruns(hub)))
	mux.HandleFunc("/api/testrun", AuthMiddleware(HandleAPITestrun(hub)))
	mux.HandleFunc("/api/testrun/wavs", AuthMiddleware(HandleAPITestrunWavs(hub)))
	mux.HandleFunc("/api/testrun/wav", AuthMiddleware(HandleAPITestrunWav(hub)))
	mux.HandleFunc("/api/testrun/download", AuthMiddleware(HandleAPITestrunDownload(hub)))
	mux.HandleFunc("/api/maintenance", AuthMiddleware(HandleAPIDatabaseMaintenance(hub.testStore)))

	// Static assets: all embedded web/* files are served automatically.
	// New JS/CSS files added to web/ need no code changes here.
	mux.HandleFunc("/", AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		StaticHandler().ServeHTTP(w, r)
	}))

	// Start HTTP server
	go func() {
		if f.TlsCert != "" && f.TlsKey != "" {
			log.Printf("Starting HTTPS server on %s", f.UiAddr)
			if err := http.ListenAndServeTLS(f.UiAddr, f.TlsCert, f.TlsKey, mux); err != nil {
				log.Fatal(err)
			}
		} else {
			log.Printf("Starting HTTP server on %s", f.UiAddr)
			if err := http.ListenAndServe(f.UiAddr, mux); err != nil {
				log.Fatal(err)
			}
		}
	}()
}

func Run() error {
	f := ParseFlags()
	MaybePrintVersionAndExit(f.Version)

	// Agent mode is signaled via environment variable for a cleaner sub-process CLI
	if os.Getenv("TELEFONIST_AGENT") == "1" {
		f.Agent = true
		f.Alias = os.Getenv("TELEFONIST_ALIAS")
		f.BaresipCtrlAddr = os.Getenv("TELEFONIST_BARESIP_CTRL")
		return RunAgent(f)
	}

	if err := os.MkdirAll(f.DataDir, 0755); err != nil {
		return err
	}

	// Set unique session cookie name based on UI port to avoid logout conflicts when running multiple instances on localhost.
	port := "8080"
	if parts := strings.Split(f.UiAddr, ":"); len(parts) > 1 {
		port = parts[len(parts)-1]
	}
	SetSessionCookieName("session_" + port)


	// Setup websocket hub
	hub := NewWsHub(f.DataDir, f.MaxCalls, f.RtpNet, f.RtpPorts, f.RtpTimeout, f.UseAlsa, f.SipListen)

	// Initialize persistent test store (SQLite next to executable) and attach to hub.
	// If it fails, we continue without persistence (UI can still use testfile_inline).
	if store, err := OpenTestStore(context.Background(), f.DataDir); err != nil {
		log.Printf("teststore: disabled: %v", err)
	} else {
		hub.SetTestStore(store)
		// Close on shutdown (best-effort).
		defer func() {
			_ = store.Close()
		}()
		log.Printf("teststore: enabled: %s", store.Path())
	}


	go hub.Run()

	startHTTPServer(f, hub)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	hub.Stop()
	hub.bm.CloseAll()
	return nil
}

