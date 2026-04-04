package telefonist

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	gobaresip "github.com/negbie/telefonist/pkg/gobaresip"
)

func RunAgent(f AppFlags) error {
	if err := os.MkdirAll(f.DataDir, 0755); err != nil {
		return err
	}

	logFile, err := SetupLogging(f.DataDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			log.Printf("failed to close log file: %v", err)
		}
	}()

	if f.SoundsDir == "" {
		f.SoundsDir = filepath.Join(f.DataDir, "sounds")
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
		gobaresip.SetAudioPath(f.SoundsDir),
		gobaresip.SetConfigPath(f.DataDir),
		gobaresip.SetAlias(f.Alias),
		gobaresip.SetUserAgent(ua),
		gobaresip.SetCtrlTCPAddr(f.CtrlAddr),
		gobaresip.SetBaresipCtrlAddr(f.BaresipCtrlAddr),
	)
}

func startHTTPServer(f AppFlags, hub *WsHub) {
	mux := http.NewServeMux()

	mux.HandleFunc("/ws", AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	mux.HandleFunc("/api/login", HandleLogin(f.UIAdminPassword))
	mux.HandleFunc("/api/logout", HandleLogout)
	mux.HandleFunc("/api/projects", AuthMiddleware(HandleAPIProjects(hub)))
	mux.HandleFunc("/api/projects/rename", AuthMiddleware(HandleAPIProjectRename(hub)))
	mux.HandleFunc("/api/projects/clone", AuthMiddleware(HandleAPIProjectClone(hub)))
	mux.HandleFunc("/api/testfiles", AuthMiddleware(HandleAPITestfiles(hub)))
	mux.HandleFunc("/api/testfile", AuthMiddleware(HandleAPITestfile(hub)))
	mux.HandleFunc("/api/testfile/rename", AuthMiddleware(HandleAPITestfileRename(hub)))
	mux.HandleFunc("/api/testruns", AuthMiddleware(HandleAPITestruns(hub)))
	mux.HandleFunc("/api/testrun", AuthMiddleware(HandleAPITestrun(hub)))
	mux.HandleFunc("/api/testrun/wavs", AuthMiddleware(HandleAPITestrunWavs(hub)))
	mux.HandleFunc("/api/testrun/wav", AuthMiddleware(HandleAPITestrunWav(hub)))
	mux.HandleFunc("/api/testrun/download", AuthMiddleware(HandleAPITestrunDownload(hub)))
	mux.HandleFunc("/api/maintenance", AuthMiddleware(HandleAPIDatabaseMaintenance(hub.testStore)))
	mux.HandleFunc("/api/project/run", HandleAPIProjectRun(hub, f.UIAPIKey))
	mux.HandleFunc("/", AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		StaticHandler().ServeHTTP(w, r)
	}))

	go func() {
		if f.TLSCert != "" && f.TLSKey != "" {
			log.Printf("Starting HTTPS server on %s", f.UIAddr)
			log.Fatal(http.ListenAndServeTLS(f.UIAddr, f.TLSCert, f.TLSKey, mux))
		}
		log.Printf("Starting HTTP server on %s", f.UIAddr)
		log.Fatal(http.ListenAndServe(f.UIAddr, mux))
	}()
}

func Run() error {
	f := ParseFlags()
	MaybePrintVersionAndExit(f.Version)

	if os.Getenv("TELEFONIST_AGENT") == "1" {
		f.Agent = true
		f.Alias = os.Getenv("TELEFONIST_ALIAS")
		f.BaresipCtrlAddr = os.Getenv("TELEFONIST_BARESIP_CTRL")
		return RunAgent(f)
	}

	if err := os.MkdirAll(f.DataDir, 0755); err != nil {
		return err
	}

	logFile, err := SetupLogging(f.DataDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := logFile.Close(); err != nil {
			log.Printf("failed to close log file: %v", err)
		}
	}()

	if f.SoundsDir == "" {
		f.SoundsDir = filepath.Join(f.DataDir, "sounds")
	}

	if err := EnsureAssets(f.SoundsDir); err != nil {
		return err
	}

	if parts := strings.Split(f.UIAddr, ":"); len(parts) > 1 {
		sessionCookieName = "session_" + parts[len(parts)-1]
	} else {
		sessionCookieName = "session_8080"
	}

	hub := NewWsHub(f.DataDir, f.MaxCalls, f.RTPNet, f.RTPPorts, f.RTPTimeout, f.UseALSA, f.SIPListen)

	store, err := OpenTestStore(context.Background(), f.DataDir)
	if err != nil {
		log.Printf("teststore: disabled: %v", err)
	} else {
		hub.SetTestStore(store)
		defer func() {
			if err := store.Close(); err != nil {
				log.Printf("failed to close test store: %v", err)
			}
		}()
		log.Printf("teststore: enabled: %s", store.Path())
	}

	go hub.Run()

	if f.UIAPIKey == "" {
		if f.UIAPIKey, err = GenerateSessionToken(); err != nil {
			return err
		}
	}
	log.Printf("UI Admin API Key (X-API-Key): %s", f.UIAPIKey)

	startHTTPServer(f, hub)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	hub.Stop()
	hub.bm.CloseAll()
	return nil
}
