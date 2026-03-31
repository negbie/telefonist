package telefonist

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// API handlers for testfile and project management.
// These replace the brittle WebSocket command system for CRUD operations.

type apiResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Token   string `json:"token,omitempty"`
}

func HandleAPIProjects(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		switch r.Method {
		case http.MethodGet:
			projects, err := hub.testStore.ListProjects(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "finished",
				"token":  "projects",
				"items":  projects,
			})

		case http.MethodPost:
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			if err := hub.testStore.SaveProject(ctx, req.Name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"projects","action":"save","message":"saved","name":%q}`, req.Name))
			json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "saved"})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}
			if err := hub.testStore.DeleteProject(ctx, name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"projects","action":"delete","message":"deleted","name":%q}`, name))
			json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func HandleAPIProjectRename(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			OldName string `json:"old_name"`
			NewName string `json:"new_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := hub.testStore.RenameProject(ctx, req.OldName, req.NewName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"projects","action":"rename","message":"renamed","old_name":%q,"new_name":%q}`, req.OldName, req.NewName))
		json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "renamed"})
	}
}

func HandleAPIProjectClone(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			SrcName    string `json:"src_name"`
			TargetName string `json:"target_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := hub.testStore.CloneProject(ctx, req.SrcName, req.TargetName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"projects","action":"clone","message":"cloned","src_name":%q,"target_name":%q}`, req.SrcName, req.TargetName))
		json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "cloned"})
	}
}

func HandleAPITestfiles(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		switch r.Method {
		case http.MethodGet:
			// List all testfiles (metadata only)
			rows, err := hub.testStore.List(ctx, false)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "finished",
				"token":  "testfiles",
				"items":  rows,
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func HandleAPITestfile(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}

			row, err := hub.testStore.Load(ctx, name, project)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			contentB64 := base64.StdEncoding.EncodeToString([]byte(row.Content))

			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "finished",
				"token":       "testfiles",
				"name":        row.Name,
				"project":     row.ProjectName,
				"content_b64": contentB64,
				"created_at":  row.CreatedAt.UTC().Format(time.RFC3339Nano),
				"updated_at":  row.UpdatedAt.UTC().Format(time.RFC3339Nano),
			})

		case http.MethodPost:
			var req struct {
				Name    string `json:"name"`
				Project string `json:"project"`
				Content string `json:"content_b64"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}

			decoded, err := base64.StdEncoding.DecodeString(req.Content)
			if err != nil {
				http.Error(w, "invalid base64", http.StatusBadRequest)
				return
			}

			if err := hub.testStore.Save(ctx, req.Name, req.Project, string(decoded)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"testfiles","action":"save","message":"saved","name":%q,"project":%q}`, req.Name, req.Project))
			json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "saved"})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}

			if err := hub.testStore.Delete(ctx, name, project); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"testfiles","action":"delete","message":"deleted","name":%q,"project":%q}`, name, project))
			json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func HandleAPITestfileRename(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			OldProject string `json:"old_project"`
			OldName    string `json:"old_name"`
			NewProject string `json:"new_project"`
			NewName    string `json:"new_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := hub.testStore.Rename(ctx, req.OldName, req.OldProject, req.NewName, req.NewProject); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"testfiles","action":"rename","message":"renamed","old_name":%q,"old_project":%q,"new_name":%q,"new_project":%q}`, req.OldName, req.OldProject, req.NewName, req.NewProject))
		json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "renamed"})
	}
}

func HandleAPITestruns(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")

			var rows []TestRunRow
			var err error

			if name == "" || name == "all" {
				rows, err = hub.testStore.ListAllRuns(ctx)
			} else {
				rows, err = hub.testStore.ListRuns(ctx, name, project)
			}

			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "finished",
				"token":  "testruns",
				"action": "list",
				"items":  rows,
			})

		case http.MethodDelete:
			// Delete all runs for a testfile
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}

			if err := hub.testStore.DeleteRunsByTestfile(ctx, name, project); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"testruns","action":"delete","testfile":%q,"project":%q,"message":"all runs deleted"}`, name, project))
			json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "all runs deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func HandleAPITestrun(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		switch r.Method {
		case http.MethodGet:
			idStr := r.URL.Query().Get("id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}

			row, err := hub.testStore.GetRun(ctx, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			contentB64 := base64.StdEncoding.EncodeToString([]byte(row.FlowEvents))

			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":          "finished",
				"token":           "testruns",
				"action":          "get",
				"id":              row.ID,
				"testfile":        row.TestfileName,
				"run_number":      row.RunNumber,
				"hash":            row.Hash,
				"result":          row.Status,
				"flow_events_b64": contentB64,
				"created_at":      row.CreatedAt.UTC().Format(time.RFC3339Nano),
			})

		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}

			if err := hub.testStore.DeleteRun(ctx, id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hub.broadcast <- []byte(fmt.Sprintf(`{"status":"finished","token":"testruns","action":"delete","id":%d,"message":"run deleted"}`, id))
			json.NewEncoder(w).Encode(apiResponse{Status: "finished", Message: "run deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func HandleAPITestrunWavs(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		wavs, err := hub.testStore.ListWavs(ctx, int64(id))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "finished",
			"token":  "testrun_wavs",
			"items":  wavs,
		})
	}
}

func HandleAPITestrunWav(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		filename, content, err := hub.testStore.GetWav(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Write(content)
	}
}

func HandleAPITestrunDownload(hub *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		downloadType := r.URL.Query().Get("type") // flow, sip, log, pcap

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		row, err := hub.testStore.GetRun(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		var events []map[string]interface{}
		// Split by double newline as done in OrderIndependentHash
		parts := strings.Split(row.FlowEvents, "\n\n")
		for _, p := range parts {
			if strings.TrimSpace(p) == "" {
				continue
			}
			var e map[string]interface{}
			if err := json.Unmarshal([]byte(p), &e); err == nil {
				events = append(events, e)
			} else {
				// Raw string broadcast
				events = append(events, map[string]interface{}{
					"type":  "RAW",
					"param": p,
				})
			}
		}

		switch downloadType {
		case "flow":
			content := filterFlowEvents(events)
			serveTextDownload(w, fmt.Sprintf("%s_%d_flow.txt", row.TestfileName, id), content)
		case "sip":
			content := filterTypedEvents(events, "SIP")
			serveTextDownload(w, fmt.Sprintf("%s_%d_sip.txt", row.TestfileName, id), content)
		case "log":
			content := filterTypedEvents(events, "LOG")
			serveTextDownload(w, fmt.Sprintf("%s_%d_log.txt", row.TestfileName, id), content)
		case "pcap":
			content := generatePcap(events)
			w.Header().Set("Content-Type", "application/vnd.tcpdump.pcap")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s_%d_sip.pcap\"", row.TestfileName, id))
			w.Write(content)
		default:
			http.Error(w, "invalid download type", http.StatusBadRequest)
		}
	}
}

const flowSeparator = "\n----------------------------------------\n\n"

var flowSkipKeys = map[string]bool{
	"event": true, "time": true, "type": true,
	"RawJSON": true, "run_id": true, "token": true,
}

func filterFlowEvents(events []map[string]interface{}) string {
	var sb strings.Builder
	first := true
	for _, e := range events {
		etype, _ := e["type"].(string)
		token, _ := e["token"].(string)

		if etype == "RAW" {
			if !first {
				sb.WriteString(flowSeparator)
			}
			first = false
			param, _ := e["param"].(string)
			sb.WriteString(param + "\n")
			continue
		}

		// Skip SIP/LOG events (they have their own download types)
		if etype == "SIP" || etype == "LOG" {
			continue
		}
		if etype == "" && (token == "SIP" || token == "LOG") {
			continue
		}

		if !first {
			sb.WriteString(flowSeparator)
		}
		first = false

		timeStr, _ := e["time"].(string)
		displayType := etype
		if displayType == "" {
			displayType = token
		}

		sb.WriteString(fmt.Sprintf("[%s] %s\n", timeStr, strings.ToUpper(displayType)))

		keys := make([]string, 0, len(e))
		for k := range e {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if flowSkipKeys[k] {
				continue
			}
			sval := fmt.Sprintf("%v", e[k])
			if sval == "" {
				continue
			}
			sb.WriteString(strings.ToUpper(k[:1]) + k[1:] + ": " + sval + "\n")
		}
	}
	return sb.String()
}

func filterTypedEvents(events []map[string]interface{}, eventType string) string {
	var sb strings.Builder
	for _, e := range events {
		t, _ := e["type"].(string)
		if t == eventType {
			param, _ := e["param"].(string)
			sb.WriteString(param)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func serveTextDownload(w http.ResponseWriter, filename, content string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Write([]byte(content))
}

func generatePcap(events []map[string]interface{}) []byte {
	var buf bytes.Buffer
	// PCAP Global Header (little-endian)
	buf.Write([]byte{
		0xd4, 0xc3, 0xb2, 0xa1, // magic
		0x02, 0x00, 0x04, 0x00, // version 2.4
		0x00, 0x00, 0x00, 0x00, // thiszone
		0x00, 0x00, 0x00, 0x00, // sigfigs
		0xff, 0xff, 0x00, 0x00, // snaplen
		0x01, 0x00, 0x00, 0x00, // Ethernet
	})

	lastSec := uint32(time.Now().Unix())
	lastUsec := uint32(0)

	for _, e := range events {
		t, _ := e["type"].(string)
		param, _ := e["param"].(string)

		// Include SIP events, and LOG events that look like SIP traces
		if t != "SIP" && !(t == "LOG" && (strings.Contains(param, "|TX ") || strings.Contains(param, "|RX "))) {
			continue
		}

		lines := strings.Split(param, "\n")
		if len(lines) < 2 {
			continue
		}

		// Extract timestamp from "2026-03-20 16:48:19|TX" or similar
		sec, usec := lastSec, lastUsec
		if idx := strings.IndexByte(lines[0], '|'); idx > 0 {
			if pt, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(lines[0][:idx]), time.Local); err == nil {
				sec = uint32(pt.Unix())
				usec = 0
			}
		}
		if sec == lastSec && usec <= lastUsec {
			usec = lastUsec + 1
		}
		lastSec, lastUsec = sec, usec

		// Find address line (first line containing "->")
		addrLine := lines[0]
		payloadIdx := 1
		if len(lines) > 1 && strings.Contains(lines[1], " -> ") {
			addrLine = lines[1]
			payloadIdx = 2
		}
		srcIP, srcPort, dstIP, dstPort := parseSipTraceLine(addrLine)

		// Build SIP payload with proper \r\n line endings
		payloadStr := strings.ReplaceAll(strings.Join(lines[payloadIdx:], "\n"), "\r\n", "\n")
		payload := []byte(strings.ReplaceAll(strings.TrimSpace(payloadStr), "\n", "\r\n") + "\r\n\r\n")

		totalLen := uint32(len(payload) + 42) // 14 eth + 20 ip + 8 udp

		// Packet Header (ts_sec, ts_usec, incl_len, orig_len)
		buf.Write([]byte{
			byte(sec), byte(sec >> 8), byte(sec >> 16), byte(sec >> 24),
			byte(usec), byte(usec >> 8), byte(usec >> 16), byte(usec >> 24),
			byte(totalLen), byte(totalLen >> 8), byte(totalLen >> 16), byte(totalLen >> 24),
			byte(totalLen), byte(totalLen >> 8), byte(totalLen >> 16), byte(totalLen >> 24),
		})

		// Ethernet Header (dummy MACs, EtherType IPv4)
		buf.Write([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x08, 0x00})

		// IP Header
		ipTotalLen := uint16(28 + len(payload)) // 20 ip + 8 udp + payload
		ipHeader := []byte{
			0x45, 0x00, // Version/IHL, TOS
			byte(ipTotalLen >> 8), byte(ipTotalLen), // Total Length
			0x00, 0x01, // ID
			0x40, 0x00, // Flags (DF), Fragment
			0x40, 17, // TTL, Protocol (UDP)
			0x00, 0x00, // Checksum (filled below)
			srcIP.To4()[0], srcIP.To4()[1], srcIP.To4()[2], srcIP.To4()[3],
			dstIP.To4()[0], dstIP.To4()[1], dstIP.To4()[2], dstIP.To4()[3],
		}
		cs := ipChecksum(ipHeader)
		ipHeader[10], ipHeader[11] = byte(cs>>8), byte(cs)
		buf.Write(ipHeader)

		// UDP Header
		udpLen := uint16(8 + len(payload))
		buf.Write([]byte{
			byte(srcPort >> 8), byte(srcPort),
			byte(dstPort >> 8), byte(dstPort),
			byte(udpLen >> 8), byte(udpLen),
			0x00, 0x00,
		})

		buf.Write(payload)
	}

	return buf.Bytes()
}

func ipChecksum(header []byte) uint16 {
	var sum uint32
	for i := 0; i < len(header); i += 2 {
		sum += uint32(header[i])<<8 | uint32(header[i+1])
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func parseSipTraceLine(line string) (net.IP, uint16, net.IP, uint16) {
	if idx := strings.IndexByte(line, '|'); idx >= 0 {
		line = line[idx+1:]
	}

	parts := strings.Fields(line)
	for i, p := range parts {
		if p != "->" || i == 0 || i >= len(parts)-1 {
			continue
		}
		sHost, sPortStr, errS := net.SplitHostPort(parts[i-1])
		dHost, dPortStr, errD := net.SplitHostPort(parts[i+1])
		if errS != nil || errD != nil {
			break
		}
		sIP := net.ParseIP(sHost)
		dIP := net.ParseIP(dHost)
		if sIP == nil || dIP == nil {
			break
		}
		sPort, _ := strconv.Atoi(sPortStr)
		dPort, _ := strconv.Atoi(dPortStr)
		return sIP, uint16(sPort), dIP, uint16(dPort)
	}
	return net.ParseIP("10.0.0.1"), 5060, net.ParseIP("10.0.0.2"), 5060
}

func HandleAPIDatabaseMaintenance(ts *TestStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := ts.Vacuum(r.Context()); err != nil {
			log.Printf("maintenance error: %v", err)
			jsonResponse(w, http.StatusInternalServerError, map[string]interface{}{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}

		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"status": "finished",
		})
	}
}

func jsonResponse(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}
