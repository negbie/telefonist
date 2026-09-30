package telefonist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RunState struct {
	Active    bool      `json:"active"`
	BatchID   string    `json:"batch_id"`
	RunID     int64     `json:"run_id"`
	File      string    `json:"file"`
	Project   string    `json:"project"`
	Step      int       `json:"step"`
	Total     int       `json:"total"`
	StartedAt time.Time `json:"started_at"`
	Message   string    `json:"message,omitempty"`
}

// Runner owns execution and admission independently of WebSocket clients.
type Runner struct {
	h            *WsHub
	mu           sync.Mutex
	state        RunState
	cancel       context.CancelFunc
	done         chan struct{}
	closed       bool
	manual       chan string
	requestMu    sync.Mutex
	requestEpoch atomic.Uint64
}

func newRunner(h *WsHub) *Runner     { return &Runner{h: h, manual: make(chan string, 16)} }
func (r *Runner) Snapshot() RunState { r.mu.Lock(); defer r.mu.Unlock(); return r.state }
func (r *Runner) publish() {
	s := r.Snapshot()
	b, _ := json.Marshal(map[string]any{"token": "runner", "state": s})
	broadcastInfo(r.h, string(b))
}
func (r *Runner) Stop() {
	r.requestEpoch.Add(1)
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Runner) StopBatch(id string) bool {
	r.mu.Lock()
	if !r.state.Active || r.state.BatchID != id {
		r.mu.Unlock()
		return false
	}
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}
func (r *Runner) Close() {
	r.mu.Lock()
	r.closed = true
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (r *Runner) Start(batch []TestfileData) bool { _, ok := r.StartWithID(batch); return ok }

func (r *Runner) StartWithID(batch []TestfileData) (string, bool) {
	if len(batch) == 0 {
		return "", false
	}
	r.mu.Lock()
	if r.closed || r.state.Active {
		r.mu.Unlock()
		return "", false
	}
	id, err := GenerateSessionToken()
	if err != nil {
		r.mu.Unlock()
		return "", false
	}
	ctx, cancel := context.WithTimeout(r.h.ctx, 30*time.Minute)
	r.state = RunState{Active: true, BatchID: id, StartedAt: time.Now()}
	r.cancel = cancel
	r.done = make(chan struct{})
	done := r.done
	r.h.inlineRunActive.Store(true)
	r.mu.Unlock()
	r.publish()
	go func() {
		defer func() {
			cancel()
			r.h.setTestCancel(nil)
			r.h.bm.CloseAll()
			r.mu.Lock()
			r.state.Active = false
			r.cancel = nil
			r.h.inlineRunActive.Store(false)
			close(done)
			r.mu.Unlock()
			r.publish()
		}()
		r.h.chainMu.Lock()
		defer r.h.chainMu.Unlock()
		for _, tf := range batch {
			if ctx.Err() != nil {
				return
			}
			r.runFile(ctx, tf, id)
		}
	}()
	return id, true
}

func runTestfilesBatch(h *WsHub, batch []TestfileData) bool {
	if h == nil {
		return false
	}
	return h.runner.Start(batch)
}

func (r *Runner) Manual(input string) {
	if r.Snapshot().Active {
		broadcastInfo(r.h, statusJSON(map[string]string{"status": "error", "token": "command", "message": "a run is active; stop it before issuing manual commands"}))
		return
	}
	select {
	case r.manual <- input:
	default:
		broadcastInfo(r.h, statusJSON(map[string]string{"status": "error", "token": "command", "message": "command queue full"}))
	}
}

func (r *Runner) manualLoop() {
	for {
		select {
		case <-r.h.ctx.Done():
			return
		case input := <-r.manual:
			r.h.chainMu.Lock()
			if !r.Snapshot().Active {
				ctx, cancel := context.WithTimeout(r.h.ctx, 30*time.Second)
				err := executeChain(ctx, r.h, parseChain(input))
				cancel()
				if err != nil {
					broadcastInfo(r.h, statusJSON(map[string]string{"status": "error", "token": "command", "message": SanitizeString(err.Error())}))
				}
			}
			r.h.chainMu.Unlock()
		}
	}
}

func (r *Runner) runFile(ctx context.Context, tf TestfileData, batch string) {
	h := r.h
	var accounts []SIPAccount
	var loadErr error
	if h.testStore != nil {
		accounts, loadErr = h.testStore.ListSIPAccounts(ctx)
	}
	cases, expected, include, repeats, ignored, accepted, webhook, parseErr := parseTestfile(tf.Content, accounts)
	skipped := repeats < 1 || len(cases) == 0
	var validator string
	if include != "" && parseErr == nil {
		data, e := os.ReadFile(filepath.Join(h.DataDir, "scripts", include))
		validator = string(data)
		if e != nil {
			parseErr = e
		}
	}
	if loadErr != nil {
		parseErr = loadErr
	}
	if parseErr != nil || repeats < 1 || len(cases) == 0 {
		repeats = 1
	}
	for rep := 1; rep <= repeats; rep++ {
		if ctx.Err() != nil {
			return
		}
		runCtx, cancel := context.WithCancel(ctx)
		h.setTestCancel(cancel)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		var id int64
		var err error
		if h.testStore == nil {
			err = errors.New("run storage unavailable")
		} else {
			id, err = h.testStore.BeginRun(cleanupCtx, tf.Name, tf.ProjectName, tf.Content, batch, rep)
		}
		cleanupCancel()
		if err != nil {
			cancel()
			broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfile", "message": "cannot create durable run: " + err.Error()}))
			return
		}
		h.bm.CloseAll()
		h.bm.SetRun(id)
		r.mu.Lock()
		r.state.RunID = id
		r.state.File = tf.Name
		r.state.Project = tf.ProjectName
		r.state.Total = len(cases)
		r.state.Step = 0
		r.mu.Unlock()
		r.publish()
		status, reason, hash, full, script := "UNVERIFIED", "", "", "", validator
		if parseErr != nil {
			status, reason = "ERROR", parseErr.Error()
		} else if skipped {
			status = "SKIPPED"
		} else {
			err = h.Do(runCtx, func() { h.trainSession = newTrainSession(ignored, accepted); h.trainSession.runID = id })
			if err == nil {
				broadcastInfo(h, statusJSON(map[string]string{"status": "running", "token": "testfile", "file": tf.Name, "project": tf.ProjectName}))
				for step, tc := range cases {
					r.mu.Lock()
					r.state.Step = step + 1
					r.mu.Unlock()
					r.publish()
					tokens := parseChain(tc.sequence)
					if len(tokens) > 0 && !tokens[len(tokens)-1].isDelay && !strings.HasPrefix(strings.ToLower(tc.sequence), "uanew ") {
						tokens = append(tokens, chainToken{delay: defaultTrailingDelay, isDelay: true})
					}
					if err = executeChain(runCtx, h, tokens); err != nil {
						reason = fmt.Sprintf("line %d: %v", tc.lineNo, err)
						break
					}
				}
			}
			// Stop writers before draining and collecting artifacts, including on
			// failure and when the script forgot uadelall.
			h.bm.CloseAll()
			finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
			drainErr := h.DrainContext(finishCtx)
			sessionErr := h.Do(finishCtx, func() {
				if s := h.trainSession; s != nil {
					hash = s.finish()
					full = s.GetFullOutput()
					if s.failMsg != "" {
						reason = s.failMsg
					}
					h.trainSession = nil
				}
			})
			finishCancel()
			switch {
			case drainErr != nil:
				status, reason = "ERROR", drainErr.Error()
			case sessionErr != nil:
				status, reason = "ERROR", sessionErr.Error()
			case ctx.Err() != nil:
				status, reason = "ABORTED", "run cancelled or deadline exceeded"
			case err != nil || reason != "":
				status = "ERROR"
				if reason == "" {
					reason = err.Error()
				}
			}
			if status == "UNVERIFIED" {
				if expected != "" {
					status = "PASS"
					if expected != hash {
						status, reason = "FAIL", "Hash mismatch"
					}
				}
				if include != "" {
					if evalErr := NewEvaluator().RunScript(runCtx, script, full); evalErr != nil {
						status, reason = "FAIL", evalErr.Error()
					} else if status == "UNVERIFIED" {
						status = "PASS"
					}
				}
			}
		}
		cancel()
		h.setTestCancel(nil)
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if e := processRecordings(persistCtx, h.testStore, id, h.DataDir); e != nil {
			status = "ERROR"
			reason += "; recordings preserved in spool: " + e.Error()
		}
		if ctx.Err() != nil {
			status = "ABORTED"
			if reason == "" {
				reason = "run cancelled"
			}
		}
		if e := h.testStore.FinishRun(persistCtx, id, status, reason, hash, full, script); e != nil {
			status = "ERROR"
			reason = "result persistence failed: " + e.Error()
		}
		keep := 100
		if value, e := h.testStore.GetSetting(persistCtx, "retention_runs"); e == nil {
			if n, e := strconv.Atoi(value); e == nil && n > 0 {
				keep = n
			}
		}
		if e := h.testStore.PruneRuns(persistCtx, keep); e != nil {
			log.Printf("retention: %v", e)
		}
		persistCancel()
		result := map[string]any{"status": "finished", "token": "testfile", "file": tf.Name, "project": tf.ProjectName, "result": status, "message": SanitizeString(reason), "run_id": id, "actual_hash": hash, "expected_hash": expected, "batch_id": batch}
		b, _ := json.Marshal(result)
		broadcastInfo(h, string(b))
		broadcastInfo(h, statusJSON(map[string]string{"status": "finished", "token": "testruns", "action": "save", "testfile": tf.Name, "project": tf.ProjectName, "id": strconv.FormatInt(id, 10)}))
		if webhook != "" {
			url := webhook
			if wh, e := h.testStore.GetWebhook(context.Background(), webhook); e == nil {
				if !wh.Enabled {
					continue
				}
				url = wh.URL
			}
			enqueueResultWebhook(h.testStore, url, tf.Name, tf.ProjectName, hash, status, id)
		}
	}
}

func processRecordings(ctx context.Context, store *TestStore, id int64, dataDir string) error {
	if store == nil || id <= 0 {
		return errors.New("recordings require a durable run")
	}
	dirs, err := filepath.Glob(filepath.Join(dataDir, "agents", fmt.Sprintf("run-%d", id), "*", "recorded_temp"))
	if err != nil {
		return err
	}
	var problems []error
	for _, dir := range dirs {
		files, e := os.ReadDir(dir)
		if e != nil {
			problems = append(problems, e)
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".wav") {
				continue
			}
			if f.Type()&os.ModeSymlink != 0 {
				problems = append(problems, errors.New("symlink recording rejected"))
				continue
			}
			filePath := filepath.Join(dir, f.Name())
			if strings.HasSuffix(f.Name(), "-enc.wav") {
				_ = os.Remove(filePath)
				continue
			}
			info, err := f.Info()
			if err != nil || info.Size() < 128 {
				_ = os.Remove(filePath)
				continue
			}
			name := strings.TrimPrefix(f.Name(), "dump-")
			if e := store.MoveWav(ctx, id, name, filePath); e != nil {
				altName := filepath.Base(filepath.Dir(dir)) + "_" + name
				if e2 := store.MoveWav(ctx, id, altName, filePath); e2 != nil {
					problems = append(problems, e)
				}
			}
		}
	}
	if len(problems) == 0 {
		runDir := filepath.Join(dataDir, "agents", fmt.Sprintf("run-%d", id))
		_ = os.RemoveAll(runDir)
	}
	return errors.Join(problems...)
}
