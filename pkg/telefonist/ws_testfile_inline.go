package telefonist

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type TestfileData struct {
	Name        string
	ProjectName string
	Content     string
}

type testCase struct {
	lineNo   int
	name     string
	sequence string
	rawLine  string
}

func handleTestfileInlineCommand(h *WsHub, input string) {
	args := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "testfile_inline")))
	if len(args) == 0 {
		broadcastInfo(h, statusJSON("status", "error", "token", "testfile", "message", "usage: testfile_inline <project|''> <name> <base64(testfile_content)>"))
		return
	}

	projectName, fileName, b64 := "", "inline", args[0]
	if len(args) >= 3 {
		projectName, fileName, b64 = args[0], args[1], args[2]
		if projectName == "''" {
			projectName = ""
		}
	} else if len(args) == 2 {
		fileName, b64 = args[0], args[1]
	}

	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		broadcastInfo(h, statusJSON("status", "error", "token", "testfile", "file", fileName, "message", "invalid base64 input: "+err.Error()))
		return
	}

	if len(decoded) > 256*1024 {
		broadcastInfo(h, statusJSON("status", "error", "token", "testfile", "file", fileName, "message", fmt.Sprintf("input too large (%d bytes), max is %d bytes", len(decoded), 256*1024)))
		return
	}

	runTestfilesBatch(h, []TestfileData{{Name: fileName, ProjectName: projectName, Content: string(decoded)}})
}

func runTestfilesBatch(h *WsHub, batch []TestfileData) bool {
	if h == nil || len(batch) == 0 {
		return false
	}

	if !h.inlineRunActive.CompareAndSwap(false, true) {
		broadcastInfo(h, statusJSON("status", "error", "token", "testfile", "message", "cannot start test: another run is already active"))
		return false
	}

	go func() {
		defer h.inlineRunActive.Store(false)

		ctx, cancel := context.WithCancel(h.ctx)
		defer cancel()

		h.internalCmd <- func() {
			h.testCancel = cancel
		}
		defer func() {
			h.internalCmd <- func() {
				h.testCancel = nil
				if h.trainSession != nil {
					h.trainSession.finish() // ensure session is closed
					h.trainSession = nil
				}
			}
			h.bm.CloseAll()
		}()

		for _, tf := range batch {
			done := make(chan struct{})
			var activeSession bool
			h.internalCmd <- func() {
				activeSession = sessionIsActive(h.trainSession)
				close(done)
			}
			select {
			case <-done:
			case <-ctx.Done():
				return
			}

			if activeSession {
				broadcastInfo(h, statusJSON("status", "error", "token", "testfile", "file", tf.Name, "message", "cannot start test: a session is already active"))
				return
			}

			runTestfileInternal(ctx, h, tf.Name, tf.ProjectName, tf.Content)

			select {
			case <-ctx.Done():
				broadcastInfo(h, fmt.Sprintf(`{"status":"stopped","token":"testfile","file":%q,"project":%q}`, tf.Name, tf.ProjectName))
				return
			default:
			}
		}
	}()

	return true
}

func runTestfileInternal(ctx context.Context, h *WsHub, fileName, projectName, content string) {
	cases, expectedGlobalHash, repeatCount, ignoredEvents, acceptedEvents, webhookURL, err := parseTestfile(content)
	if err != nil {
		broadcastInfo(h, fmt.Sprintf(`{"status":"error","token":"testfile","file":%q,"project":%q,"message":%q}`, fileName, projectName, err.Error()))
		return
	}

	if len(cases) == 0 {
		broadcastInfo(h, fmt.Sprintf(`{"status":"finished","token":"testfile","file":%q,"project":%q,"total":0,"result":"PASS"}`, fileName, projectName))
		return
	}

	log.Printf("running testfile: %s (project %s) (%d cases, %d repeats)", fileName, projectName, len(cases), repeatCount)

	// Stop all active agents before starting a test run to ensure a clean state
	h.bm.CloseAll()

	for rep := 1; rep <= repeatCount; rep++ {
		select {
		case <-ctx.Done():
			checkTestFailure(h, fileName, projectName)
			return
		default:
		}

		broadcastInfo(h, fmt.Sprintf(`{"status":"running","token":"testfile","file":%q,"project":%q,"total":%d}`, fileName, projectName, len(cases)))

		sessionReady := make(chan struct{})
		h.internalCmd <- func() {
			h.trainSession = newTrainSession(ignoredEvents, acceptedEvents)
			close(sessionReady)
		}
		select {
		case <-sessionReady:
		case <-ctx.Done():
			checkTestFailure(h, fileName, projectName)
			return
		}

		for _, tc := range cases {
			select {
			case <-ctx.Done():
				checkTestFailure(h, fileName, projectName)
				return
			default:
			}

			tokens := parseChain(tc.sequence)
			if len(tokens) > 0 && !tokens[len(tokens)-1].isDelay {
				tokens = append(tokens, chainToken{delay: defaultTrailingDelay, isDelay: true})
			}

			h.chainMu.Lock()
			executeChain(ctx, h, tokens)
			h.chainMu.Unlock()
		}

		// Ensure all events from this run are processed by WsHub.run
		h.Drain()

		var actualHash string
		var fullLog string
		done := make(chan struct{})
		h.internalCmd <- func() {
			if h.trainSession != nil {
				actualHash = h.trainSession.finish()
				fullLog = h.trainSession.GetFullOutput()
				h.trainSession = nil
			}
			close(done)
		}

		select {
		case <-done:
		case <-ctx.Done():
			checkTestFailure(h, fileName, projectName)
			return
		}

		status := "PASS"
		if expectedGlobalHash != "" && actualHash != expectedGlobalHash {
			status = "FAIL"
		}

		var runID int64
		if store := h.testStore; store != nil {
			if id, err := store.SaveRun(context.Background(), fileName, projectName, rep, actualHash, status, fullLog); err != nil {
				log.Printf("failed to save run: %v", err)
			} else {
				runID = id
				h.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"testruns","action":"save","testfile":%q,"project":%q}`, fileName, projectName))
			}
		}

		// Collect final recordings after all steps (including uadelall) are done and agents are stopped
		processRecordings(ctx, h.testStore, runID, h.DataDir)

		broadcastInfo(h, fmt.Sprintf(
			`{"status":"finished","token":"testfile","file":%q,"project":%q,"total":%d,"expected_hash":%q,"actual_hash":%q,"result":%q,"run_id":%d}`,
			fileName, projectName, len(cases), expectedGlobalHash, actualHash, status, runID,
		))

		if webhookURL != "" {
			go func() {
				if err := sendResultWebhook(webhookURL, fileName, projectName, actualHash, status, runID); err != nil {
					log.Printf("failed to send result webhook: %v", err)
				}
			}()
		}

		log.Printf("--- Finished: %s [%s] --- Project: %s, Hash: %s, Run: %d", fileName, status, projectName, actualHash, runID)
	}
}

func checkTestFailure(h *WsHub, fileName, projectName string) {
	var failMsg string
	done := make(chan struct{})
	h.internalCmd <- func() {
		if h.trainSession != nil {
			failMsg = h.trainSession.failMsg
			h.trainSession.finish() // ensure session is closed even if stopped
			h.trainSession = nil
		}
		close(done)
	}
	<-done

	if failMsg != "" {
		broadcastInfo(h, fmt.Sprintf(
			`{"status":"finished","token":"testfile","file":%q,"project":%q,"result":"FAIL","message":%q}`,
			fileName, projectName, failMsg,
		))
	} else {
		broadcastInfo(h, fmt.Sprintf(`{"status":"stopped","token":"testfile","file":%q,"project":%q}`, fileName, projectName))
	}
}

func parseTestfile(content string) (cases []testCase, expectedHash string, repeatCount int, ignoredEvents []string, acceptedEvents []string, webhookURL string, err error) {
	repeatCount = 1
	defines := make(map[string]string)
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		lowerLine := strings.ToLower(line)
		if strings.HasPrefix(lowerLine, "_hash ") || strings.HasPrefix(lowerLine, "hash:") {
			if strings.HasPrefix(lowerLine, "_hash ") {
				expectedHash = strings.TrimSpace(line[6:])
			} else {
				expectedHash = strings.TrimSpace(line[5:])
			}
			continue
		}

		if strings.HasPrefix(lowerLine, "_ignore ") {
			parts := strings.Split(line[8:], ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					ignoredEvents = append(ignoredEvents, p)
				}
			}
			continue
		}

		if strings.HasPrefix(lowerLine, "_accept ") {
			parts := strings.Split(line[8:], ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					acceptedEvents = append(acceptedEvents, p)
				}
			}
			continue
		}

		if strings.HasPrefix(lowerLine, "_define ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				defines[parts[1]] = strings.Join(parts[2:], " ")
			}
			continue
		}

		if strings.HasPrefix(lowerLine, "_webhook ") {
			webhookURL = strings.TrimSpace(line[9:])
			continue
		}

		if strings.HasPrefix(lowerLine, "_run ") {
			parts := strings.Fields(line)
			if len(parts) == 2 {
				if r, err := strconv.Atoi(parts[1]); err == nil {
					if r < 1 {
						// Skip this test file entirely
						return nil, "", 0, nil, nil, "", nil
					}
					repeatCount = r
					continue
				}
			}
			continue
		}

		name := ""
		sequence := line

		// Sort keys by length descending to ensure longer variable names are replaced first
		keys := make([]string, 0, len(defines))
		for k := range defines {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return len(keys[i]) > len(keys[j])
		})

		for _, k := range keys {
			sequence = strings.ReplaceAll(sequence, k, defines[k])
		}

		cases = append(cases, testCase{
			lineNo:   lineNo,
			name:     name,
			sequence: sequence,
			rawLine:  raw,
		})
	}
	return cases, expectedHash, repeatCount, ignoredEvents, acceptedEvents, webhookURL, sc.Err()
}

func processRecordings(ctx context.Context, store *TestStore, runID int64, dataDir string) {
	// Find all agent-specific recorded_temp directories
	pattern := filepath.Join(dataDir, "agents", "*", "recorded_temp")
	dirs, err := filepath.Glob(pattern)
	if err != nil {
		log.Printf("failed to glob agent recording dirs: %v", err)
		return
	}

	for _, recordsDir := range dirs {
		files, err := os.ReadDir(recordsDir)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("failed to read records dir %s: %v", recordsDir, err)
			}
			continue
		}

		if len(files) == 0 {
			continue
		}

		log.Printf("processing recordings in %s (found %d files)", recordsDir, len(files))
		for _, f := range files {
			if f.IsDir() {
				continue
			}

			path := filepath.Join(recordsDir, f.Name())

			// Skip and remove too small WAV files
			info, err := f.Info()
			if err != nil {
				log.Printf("failed to stat recorded file %s: %v", f.Name(), err)
				continue
			}
			if info.Size() < 128 {
				log.Printf("skipping and removing too small recorded file: %s (%d bytes)", f.Name(), info.Size())
				os.Remove(path)
				continue
			}

			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("failed to read recorded file %s: %v", path, err)
				continue
			}

			newName := strings.TrimPrefix(f.Name(), "dump-")
			if !strings.HasSuffix(newName, "-enc.wav") {
				if err := store.SaveWav(ctx, runID, newName, data); err != nil {
					log.Printf("failed to save wav %s to db: %v", newName, err)
					continue
				}
			}

			if err := os.Remove(path); err != nil {
				log.Printf("failed to remove recorded file %s: %v", path, err)
			} else {
				log.Printf("captured and stored recording: %s", newName)
			}
		}

		agentDir := filepath.Dir(recordsDir)
		if err := os.RemoveAll(agentDir); err != nil {
			log.Printf("failed to cleanup agent directory %s: %v", agentDir, err)
		} else {
			log.Printf("cleaned up agent directory: %s", agentDir)
		}
	}
}
