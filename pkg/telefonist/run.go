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
	"time"

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

func newHTTPHandler(f AppFlags, hub *WsHub) http.Handler {
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
	mux.HandleFunc("/api/testfile/versions", AuthMiddleware(HandleAPITestfileVersions(hub)))
	mux.HandleFunc("/api/testfiles/clone", AuthMiddleware(HandleAPITestfileClone(hub)))
	mux.HandleFunc("/api/testruns", AuthMiddleware(HandleAPITestruns(hub)))
	mux.HandleFunc("/api/testrun", AuthMiddleware(HandleAPITestrun(hub)))
	mux.HandleFunc("/api/testrun/wavs", AuthMiddleware(HandleAPITestrunWavs(hub)))
	mux.HandleFunc("/api/testrun/wav", AuthMiddleware(HandleAPITestrunWav(hub)))
	mux.HandleFunc("/api/testrun/download", AuthMiddleware(HandleAPITestrunDownload(hub)))
	mux.HandleFunc("/api/maintenance", AuthMiddleware(HandleAPIDatabaseMaintenance(hub.testStore)))
	mux.HandleFunc("/api/project/run", HandleAPIProjectRun(hub, f.UIAPIKey))
	mux.HandleFunc("/api/runner", AuthMiddleware(HandleRunner(hub)))
	mux.HandleFunc("/api/operations", AuthMiddleware(HandleOperations(hub)))
	mux.HandleFunc("/api/batch", APIKeyOrSession(f.UIAPIKey, HandleBatch(hub)))
	mux.HandleFunc("/api/batch/junit", APIKeyOrSession(f.UIAPIKey, HandleBatchJUnit(hub)))
	mux.HandleFunc("/api/cron", AuthMiddleware(HandleAPICronJobs(hub)))
	mux.HandleFunc("/api/cron/modify", AuthMiddleware(HandleAPICronJobModify(hub)))
	mux.HandleFunc("/api/accounts", AuthMiddleware(HandleAPISIPAccounts(hub)))
	mux.HandleFunc("/api/accounts/delete", AuthMiddleware(HandleAPISIPAccountDelete(hub)))
	mux.HandleFunc("/api/webhooks", AuthMiddleware(HandleAPIWebhooks(hub)))
	mux.HandleFunc("/api/webhooks/delete", AuthMiddleware(HandleAPIWebhookDelete(hub)))
	mux.HandleFunc("/api/settings/log-filter", AuthMiddleware(HandleAPILogFilter(hub)))
	mux.HandleFunc("/", AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		StaticHandler().ServeHTTP(w, r)
	}))

	return limitRequests(mux)
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
		return fmt.Errorf("test store unavailable: %w", err)
	} else {
		hub.SetTestStore(store)
		defer func() {
			if err := store.Close(); err != nil {
				log.Printf("failed to close test store: %v", err)
			}
		}()
		log.Printf("teststore: enabled: %s", store.Path())

		cm := NewCronManager(hub, store)
		cm.Start()
		hub.SetCronManager(cm)
		defer cm.Stop()
	}

	if err := ensureCredentials(&f); err != nil {
		return err
	}
	go hub.Run()
	defer func() { hub.Stop(); <-hub.done; hub.bm.CloseAll() }()
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := runWebhookWorker(workerCtx, store)
	defer func() { workerCancel(); <-workerDone }()
	server := &http.Server{Addr: f.UIAddr, Handler: newHTTPHandler(f, hub), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	serverErrors := make(chan error, 1)
	go func() {
		if f.TLSCert != "" && f.TLSKey != "" {
			serverErrors <- server.ListenAndServeTLS(f.TLSCert, f.TLSKey)
		} else {
			serverErrors <- server.ListenAndServe()
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	var serveErr error
	select {
	case <-sig:
	case err := <-serverErrors:
		if err != http.ErrServerClosed {
			log.Printf("HTTP server: %v", err)
			serveErr = err
		}
	}
	if hub.cronManager != nil {
		hub.cronManager.Stop()
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)

	hub.Stop()
	hub.bm.CloseAll()
	return serveErr
}
