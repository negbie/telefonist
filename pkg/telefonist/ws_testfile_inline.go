package telefonist

import (
	"bufio"
	"encoding/base64"
	"fmt"
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
		if lowerLine == "_hash" {
			expectedHash = ""
			continue
		}
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
			if !validArtifactName(includeScript) {
				err = fmt.Errorf("line %d: include must be a script filename", lineNo)
				return
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
			} else {
				err = fmt.Errorf("line %d: _define requires a name and value", lineNo)
				return
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
					if r > 1000 {
						return nil, "", "", 0, nil, nil, "", fmt.Errorf("line %d: at most 1000 repeats allowed", lineNo)
					}
					repeatCount = r
					continue
				}
			}
			err = fmt.Errorf("line %d: invalid _run count", lineNo)
			return
		}
		if strings.HasPrefix(line, "_") {
			err = fmt.Errorf("line %d: unknown or incomplete directive", lineNo)
			return
		}

		name := ""
		sequence := line

		// Sort keys by length descending to ensure longer variable names are replaced first
		keys := make([]string, 0, len(defines))
		for k := range defines {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if len(keys[i]) == len(keys[j]) {
				return keys[i] < keys[j]
			}
			return len(keys[i]) > len(keys[j])
		})

		var replacements []string
		for _, k := range keys {
			replacements = append(replacements, k, defines[k])
		}
		sequence = strings.NewReplacer(replacements...).Replace(sequence)

		sequence = resolveAccountsInSequence(sequence, accounts)
		for _, part := range splitByPipe(sequence) {
			part = strings.TrimSpace(part)
			if isDuration(part) {
				if d, e := time.ParseDuration(part); e != nil || d > time.Hour {
					err = fmt.Errorf("line %d: invalid or excessive delay", lineNo)
					return
				}
			} else if !isKnownCommand(part) {
				err = fmt.Errorf("line %d: unknown command %q", lineNo, SanitizeString(part))
				return
			}
		}

		cases = append(cases, testCase{
			lineNo:   lineNo,
			name:     name,
			sequence: sequence,
			rawLine:  raw,
		})
	}
	return cases, expectedHash, includeScript, repeatCount, ignoredEvents, acceptedEvents, webhookURL, sc.Err()
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
						paramPart = strings.TrimPrefix(rest[idx+1:], ";")
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
			sequence = strings.ReplaceAll(sequence, placeholder, replacement)
		}
	}

	// Resolve complete command targets and the first URI argument only. Message
	// bodies and overlapping account names must remain untouched.
	parts = splitByPipe(sequence)
	for i, part := range parts {
		part = strings.TrimSpace(part)
		for _, acc := range accounts {
			if strings.HasPrefix(part, acc.Name+":") {
				part = acc.SIPURI + normalizeParam(acc.URIParams) + part[len(acc.Name):]
				break
			}
		}
		fields := strings.Fields(part)
		if len(fields) > 1 {
			command := fields[0]
			if n := strings.LastIndex(command, ":"); n >= 0 {
				command = command[n+1:]
			}
			switch strings.ToLower(command) {
			case "dial", "dialdir", "transfer", "atransferstart", "options", "uafind":
				for _, acc := range accounts {
					if fields[1] == acc.Name {
						start := len(fields[0]) + strings.Index(part[len(fields[0]):], fields[1])
						part = part[:start] + acc.SIPURI + normalizeParam(acc.URIParams) + part[start+len(fields[1]):]
						break
					}
				}
			}
		}
		parts[i] = part
	}
	return strings.Join(parts, "|")
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
