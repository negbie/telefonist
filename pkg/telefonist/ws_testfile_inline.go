package telefonist

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfile", "message": "usage: testfile_inline <project|''> <name> <base64(testfile_content)>"}))
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
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfile", "file": fileName, "message": "invalid base64 input: " + err.Error()}))
		return
	}

	if len(decoded) > 256*1024 {
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfile", "file": fileName, "message": fmt.Sprintf("input too large (%d bytes), max is %d bytes", len(decoded), 256*1024)}))
		return
	}

	runTestfilesBatch(h, []TestfileData{{Name: fileName, ProjectName: projectName, Content: string(decoded)}})
}

func runTestfilesBatch(h *WsHub, batch []TestfileData) bool {
	if h == nil || len(batch) == 0 {
		return false
	}

	if !h.inlineRunActive.CompareAndSwap(false, true) {
		broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfile", "message": "cannot start test: another run is already active"}))
		return false
	}

	go func() {
		defer h.inlineRunActive.Store(false)

		ctx, cancel := context.WithCancel(h.ctx)
		defer cancel()

		h.internalCmd <- func() {
			h.batchCancel = cancel
		}
		defer func() {
			h.internalCmd <- func() {
				h.batchCancel = nil
				h.testCancel = nil
				if h.trainSession != nil {
					h.trainSession.finish()
					h.trainSession = nil
				}
			}
			h.bm.CloseAll()
		}()

		for _, tf := range batch {
			// Check if the entire batch was canceled (e.g. via test_stop)
			select {
			case <-ctx.Done():
				return
			default:
			}

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
				broadcastInfo(h, statusJSON(map[string]string{"status": "error", "token": "testfile", "file": tf.Name, "message": "cannot start test: a session is already active"}))
				continue // Try next test in batch instead of aborting everything
			}

			// Create a per-test context that is also canceled if the batch is canceled
			testCtx, testCancel := context.WithCancel(ctx)
			h.internalCmd <- func() {
				h.testCancel = testCancel
			}

			runTestfileInternal(testCtx, h, tf.Name, tf.ProjectName, tf.Content)
			testCancel() // Cleanup per-test context

			h.internalCmd <- func() {
				h.testCancel = nil
			}

			// Small pause between tests for cleanup
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	}()

	return true
}

func runTestfileInternal(ctx context.Context, h *WsHub, fileName, projectName, content string) {
	var accounts []SIPAccount
	if h.testStore != nil {
		var err error
		accounts, err = h.testStore.ListSIPAccounts(ctx)
		if err != nil {
			log.Printf("error loading centralized SIP accounts: %v", err)
		}
	}
	cases, expectedGlobalHash, includeScript, repeatCount, ignoredEvents, acceptedEvents, webhookURL, err := parseTestfile(content, accounts)
	if err != nil {
		broadcastInfo(h, fmt.Sprintf(`{"status":"error","token":"testfile","file":%q,"project":%q,"message":%q}`, fileName, projectName, err.Error()))
		return
	}

	if len(cases) == 0 {
		broadcastInfo(h, fmt.Sprintf(`{"status":"finished","token":"testfile","file":%q,"project":%q,"total":0,"result":"PASS"}`, fileName, projectName))
		return
	}

	log.Printf("running testfile: %s (project %s) (%d cases, %d repeats, include: %s)", fileName, projectName, len(cases), repeatCount, includeScript)

	// Stop all active agents before starting a test run to ensure a clean state
	h.bm.CloseAll()

	for rep := 1; rep <= repeatCount; rep++ {
		var actualHash, fullLog, status, failReason string
		var runID int64

		broadcastInfo(h, fmt.Sprintf(`{"status":"running","token":"testfile","file":%q,"project":%q,"total":%d}`, fileName, projectName, len(cases)))

		// Start session
		sessionReady := make(chan struct{})
		h.internalCmd <- func() {
			h.trainSession = newTrainSession(ignoredEvents, acceptedEvents)
			close(sessionReady)
		}
		select {
		case <-sessionReady:
		case <-ctx.Done():
			failReason = checkTestFailure(h, fileName, projectName)
			goto finish_run
		}

		// Run cases
		for _, tc := range cases {
			select {
			case <-ctx.Done():
				failReason = checkTestFailure(h, fileName, projectName)
				goto finish_run
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

		// Finish session and get results
		{
			done := make(chan struct{})
			h.internalCmd <- func() {
				if h.trainSession != nil {
					actualHash = h.trainSession.finish()
					fullLog = h.trainSession.GetFullOutput()
					failReason = h.trainSession.failMsg
					h.trainSession = nil
				}
				close(done)
			}
			select {
			case <-done:
			case <-ctx.Done():
				failReason = checkTestFailure(h, fileName, projectName)
				goto finish_run
			}
		}

		status = "PASS"
		if failReason != "" {
			status = "FAIL"
		} else if includeScript != "" {
			scriptPath := filepath.Join(h.DataDir, "scripts", filepath.Base(includeScript))
			scriptBytes, err := os.ReadFile(scriptPath)
			if err != nil {
				status = "FAIL"
				failReason = fmt.Sprintf("failed to read include script %s: %v", includeScript, err)
			} else {
				eval := NewEvaluator()
				if err := eval.RunScript(ctx, string(scriptBytes), fullLog); err != nil {
					status = "FAIL"
					failReason = err.Error()
				}
			}
		} else if expectedGlobalHash != "" && actualHash != expectedGlobalHash {
			status = "FAIL"
			failReason = "Hash mismatch"
		}

	finish_run:
		if ctx.Err() != nil {
			st := "finished"
			msg := failReason
			if msg == "" {
				st = "stopped"
			}
			resultMsg := map[string]interface{}{
				"status":        st,
				"token":         "testfile",
				"file":          fileName,
				"project":       projectName,
				"total":         len(cases),
				"expected_hash": expectedGlobalHash,
				"actual_hash":   actualHash,
				"result":        "FAIL",
			}
			if msg != "" {
				resultMsg["message"] = msg
			}
			b, _ := json.Marshal(resultMsg)
			broadcastInfo(h, string(b))
			return
		}

		if failReason != "" && status == "" {
			status = "FAIL"
		}

		if store := h.testStore; store != nil {
			if id, err := store.SaveRun(context.Background(), fileName, projectName, rep, actualHash, status, fullLog); err != nil {
				log.Printf("failed to save run: %v", err)
			} else {
				runID = id
				h.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"testruns","action":"save","testfile":%q,"project":%q}`, fileName, projectName))
			}
		}

		// Collect final recordings
		processRecordings(ctx, h.testStore, runID, h.DataDir)

		// Broadcast final result to UI
		resultMsg := map[string]interface{}{
			"status":        "finished",
			"token":         "testfile",
			"file":          fileName,
			"project":       projectName,
			"total":         len(cases),
			"expected_hash": expectedGlobalHash,
			"actual_hash":   actualHash,
			"result":        status,
			"run_id":        runID,
		}
		if failReason != "" {
			resultMsg["message"] = failReason
		}
		b, _ := json.Marshal(resultMsg)
		broadcastInfo(h, string(b))

		if webhookURL != "" {
			go func() {
				if err := sendResultWebhook(webhookURL, fileName, projectName, actualHash, status, runID); err != nil {
					log.Printf("failed to send result webhook: %v", err)
				}
			}()
		}

		log.Printf("--- Finished: %s [%s] --- Project: %s, Hash: %s, Run: %d", fileName, status, projectName, actualHash, runID)

		// If the context was canceled, don't do more repeats
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func checkTestFailure(h *WsHub, fileName, projectName string) string {
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

	if failMsg == "" {
		broadcastInfo(h, fmt.Sprintf(`{"status":"stopped","token":"testfile","file":%q,"project":%q}`, fileName, projectName))
	}
	return failMsg
}

func parseTestfile(content string, accounts []SIPAccount) (cases []testCase, expectedHash string, includeScript string, repeatCount int, ignoredEvents []string, acceptedEvents []string, webhookURL string, err error) {
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

		if strings.HasPrefix(lowerLine, "_include ") {
			includeScript = strings.TrimSpace(line[9:])
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
						return nil, "", "", 0, nil, nil, "", nil
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

		sequence = resolveAccountsInSequence(sequence, accounts)

		cases = append(cases, testCase{
			lineNo:   lineNo,
			name:     name,
			sequence: sequence,
			rawLine:  raw,
		})
	}
	return cases, expectedHash, includeScript, repeatCount, ignoredEvents, acceptedEvents, webhookURL, sc.Err()
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

func resolveAccountsInSequence(sequence string, accounts []SIPAccount) string {
	if len(accounts) == 0 {
		return sequence
	}

	// 1. Process uanew commands (handling bracketless and bracketed names, appending default params & auth_pass)
	parts := splitByPipe(sequence)
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(trimmed), "uanew ") {
			rest := strings.TrimSpace(trimmed[6:])
			aorPart := rest
			paramPart := ""
			if strings.HasPrefix(rest, "<") {
				idx := strings.Index(rest, ">")
				if idx != -1 {
					aorPart = rest[:idx+1]
					if idx+1 < len(rest) {
						paramPart = rest[idx+1:]
						if strings.HasPrefix(paramPart, ";") {
							paramPart = paramPart[1:]
						}
					}
				}
			} else {
				aorAndParams := strings.SplitN(rest, ";", 2)
				aorPart = strings.TrimSpace(aorAndParams[0])
				if len(aorAndParams) > 1 {
					paramPart = strings.TrimSpace(aorAndParams[1])
				}
			}

			nameOrURI := aorPart
			if strings.HasPrefix(aorPart, "<") && strings.HasSuffix(aorPart, ">") {
				nameOrURI = aorPart[1 : len(aorPart)-1]
			}

			// Try to find the account by friendly name first
			var foundAcc *SIPAccount
			for _, acc := range accounts {
				if acc.Name == nameOrURI {
					foundAcc = &acc
					break
				}
			}

			// If not found by name, try to find by SIP URI/AOR matching
			if foundAcc == nil {
				extractedAOR := ExtractAlias("<" + nameOrURI + ">")
				if extractedAOR != "" {
					for _, acc := range accounts {
						accAOR := ExtractAlias("<" + acc.SIPURI + ">")
						if accAOR != "" && accAOR == extractedAOR {
							foundAcc = &acc
							break
						}
					}
				}
			}

			if foundAcc != nil {
				resolvedAOR := "<" + foundAcc.SIPURI + normalizeParam(foundAcc.URIParams) + ">"
				addrParams := ""
				if foundAcc.Password != "" {
					addrParams += ";auth_pass=" + foundAcc.Password
				}
				if foundAcc.AddrParams != "" {
					addrParams += normalizeParam(foundAcc.AddrParams)
				}
				if paramPart != "" {
					addrParams += ";" + paramPart
				}
				resolvedAOR += normalizeParam(addrParams)
				parts[i] = "uanew " + resolvedAOR
			}
		}
	}
	sequence = strings.Join(parts, "|")

	// 2. Replace <account_name> inside angle brackets (for other parts of script)
	for _, acc := range accounts {
		placeholder := "<" + acc.Name + ">"
		if strings.Contains(sequence, placeholder) {
			replacement := "<" + acc.SIPURI + normalizeParam(acc.URIParams) + ">"
			if acc.Password != "" {
				replacement += ";auth_pass=" + acc.Password
			}
			if acc.AddrParams != "" {
				replacement += normalizeParam(acc.AddrParams)
			}
			sequence = strings.ReplaceAll(sequence, placeholder, replacement)
		}
	}

	// 3. Replace prefix account_name: (at start of command, or after a pipe)
	for _, acc := range accounts {
		fullSIPURI := acc.SIPURI + normalizeParam(acc.URIParams)
		prefix := acc.Name + ":"
		replacement := fullSIPURI + ":"
		// Case 1: Start of sequence
		if strings.HasPrefix(sequence, prefix) {
			sequence = replacement + sequence[len(prefix):]
		}
		// Case 2: After a pipe
		sequence = strings.ReplaceAll(sequence, "|"+prefix, "|"+replacement)
		// Case 3: After a pipe with space
		sequence = strings.ReplaceAll(sequence, "| "+prefix, "| "+replacement)
	}

	// 4. Replace whole-word arguments in commands (like dial destination).
	for _, acc := range accounts {
		fullSIPURI := acc.SIPURI + normalizeParam(acc.URIParams)
		sequence = strings.ReplaceAll(sequence, " "+acc.Name, " "+fullSIPURI)
	}

	return sequence
}

func normalizeParam(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	for strings.Contains(p, ";;") {
		p = strings.ReplaceAll(p, ";;", ";")
	}
	if !strings.HasPrefix(p, ";") {
		p = ";" + p
	}
	p = strings.TrimRight(p, "; ")
	return p
}

