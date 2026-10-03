package telefonist

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

type apiResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Token   string `json:"token,omitempty"`
}

// storeHandler is the signature for handlers that need a TestStore with a context.
type storeHandler func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context)

func isChangingIdentity(r *http.Request) bool {
	return r.URL.Path == "/api/maintenance"
}

func activeRunUsesIdentity(hub *WsHub, file, project string) bool {
	if hub.runner == nil {
		return false
	}
	state := hub.runner.Snapshot()
	return state.Active && state.Project == project && (file == "" || state.File == file)
}

// withStore wraps a handler to check testStore availability and create a 5s timeout context.
func withStore(hub *WsHub, fn storeHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && hub.runner != nil && hub.runner.Snapshot().Active && isChangingIdentity(r) {
			jsonResponse(w, http.StatusConflict, apiResponse{Status: "error", Message: "stop the active run before changing test identities"})
			return
		}
		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		fn(w, r, hub.testStore, ctx)
	}
}

// cronHandler is the signature for handlers that need both TestStore and CronManager.
type cronHandler func(w http.ResponseWriter, r *http.Request, store *TestStore, cm *CronManager, ctx context.Context)

// withCron wraps a handler to check testStore + cronManager availability and create a 5s timeout context.
func withCron(hub *WsHub, fn cronHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hub.testStore == nil || hub.cronManager == nil {
			http.Error(w, "cron not enabled", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		fn(w, r, hub.testStore, hub.cronManager, ctx)
	}
}

func HandleAPIProjects(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			projects, err := store.ListProjects(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{
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
			req.Name = SanitizeName(req.Name)
			if err := store.SaveProject(ctx, req.Name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "save", "message": "saved", "name": req.Name}))
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "saved"})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}
			if activeRunUsesIdentity(hub, "", name) {
				jsonResponse(w, http.StatusConflict, apiResponse{Status: "error", Message: "stop the active run before changing test identities"})
				return
			}
			if err := store.DeleteProject(ctx, name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "delete", "message": "deleted", "name": name}))
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPIProjectRename(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
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

		req.OldName = SanitizeName(req.OldName)
		req.NewName = SanitizeName(req.NewName)

		if activeRunUsesIdentity(hub, "", req.OldName) {
			jsonResponse(w, http.StatusConflict, apiResponse{Status: "error", Message: "stop the active run before changing test identities"})
			return
		}

		if err := store.RenameProject(ctx, req.OldName, req.NewName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "rename", "message": "renamed", "old_name": req.OldName, "new_name": req.NewName}))
		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "renamed"})
	})
}

func HandleAPIProjectClone(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
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

		req.SrcName = SanitizeName(req.SrcName)
		req.TargetName = SanitizeName(req.TargetName)

		if err := store.CloneProject(ctx, req.SrcName, req.TargetName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "clone", "message": "cloned", "src_name": req.SrcName, "target_name": req.TargetName}))
		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "cloned"})
	})
}

func HandleAPITestfiles(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			rows, err := store.List(ctx, false)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{
				"status": "finished",
				"token":  "testfiles",
				"items":  rows,
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPITestfile(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}

			row, err := store.Load(ctx, name, project)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			jsonResponse(w, http.StatusOK, map[string]any{
				"status":      "finished",
				"token":       "testfiles",
				"name":        row.Name,
				"project":     row.ProjectName,
				"content_b64": base64.StdEncoding.EncodeToString([]byte(row.Content)),
				"created_at":  row.CreatedAt.Format(time.RFC3339Nano),
				"updated_at":  row.UpdatedAt.Format(time.RFC3339Nano),
			})

		case http.MethodPost:
			var req struct {
				Name     string  `json:"name"`
				Project  string  `json:"project"`
				Content  string  `json:"content_b64"`
				Revision *string `json:"revision"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				log.Printf("api: failed to decode save testfile request: %v", err)
				http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
				return
			}

			req.Name = SanitizeName(req.Name)
			req.Project = SanitizeName(req.Project)

			decoded, err := base64.StdEncoding.DecodeString(req.Content)
			if err != nil {
				http.Error(w, "invalid base64", http.StatusBadRequest)
				return
			}

			if err := store.SaveExpected(ctx, req.Name, req.Project, string(decoded), req.Revision); err != nil {
				if errors.Is(err, ErrSaveConflict) {
					jsonResponse(w, http.StatusConflict, apiResponse{Status: "error", Message: err.Error()})
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "testfiles", "action": "save", "message": "saved", "name": req.Name, "project": req.Project}))
			row, err := store.Load(ctx, req.Name, req.Project)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{"status": "finished", "message": "saved", "revision": row.UpdatedAt.Format(time.RFC3339Nano)})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}
			if activeRunUsesIdentity(hub, name, project) {
				jsonResponse(w, http.StatusConflict, apiResponse{Status: "error", Message: "stop the active run before changing test identities"})
				return
			}

			if err := store.Delete(ctx, name, project); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "testfiles", "action": "delete", "message": "deleted", "name": name, "project": project}))
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPITestfileRename(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
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

		req.OldProject = SanitizeName(req.OldProject)
		req.OldName = SanitizeName(req.OldName)
		req.NewProject = SanitizeName(req.NewProject)
		req.NewName = SanitizeName(req.NewName)

		if activeRunUsesIdentity(hub, req.OldName, req.OldProject) {
			jsonResponse(w, http.StatusConflict, apiResponse{Status: "error", Message: "stop the active run before changing test identities"})
			return
		}

		if err := store.Rename(ctx, req.OldName, req.OldProject, req.NewName, req.NewProject); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "testfiles", "action": "rename", "message": "renamed", "old_name": req.OldName, "old_project": req.OldProject, "new_name": req.NewName, "new_project": req.NewProject}))
		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "renamed"})
	})
}

func HandleAPITestfileVersions(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}

			list, err := store.ListVersions(ctx, name, project)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			type versionItem struct {
				ID         int64  `json:"id"`
				CreatedAt  string `json:"created_at"`
				ContentB64 string `json:"content_b64"`
			}

			versions := make([]versionItem, len(list))
			for i, v := range list {
				versions[i] = versionItem{
					ID:         v.ID,
					CreatedAt:  v.CreatedAt.Format(time.RFC3339Nano),
					ContentB64: base64.StdEncoding.EncodeToString([]byte(v.Content)),
				}
			}

			jsonResponse(w, http.StatusOK, map[string]any{
				"status":   "finished",
				"versions": versions,
			})

		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			if idStr == "" {
				http.Error(w, "id required", http.StatusBadRequest)
				return
			}
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}
			if err := store.DeleteVersion(ctx, id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "version deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPITestfileClone(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Project    string `json:"project"`
			SrcName    string `json:"src_name"`
			TargetName string `json:"target_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		req.Project = SanitizeName(req.Project)
		req.SrcName = SanitizeName(req.SrcName)
		req.TargetName = SanitizeName(req.TargetName)

		if err := store.CloneTestfile(ctx, req.Project, req.SrcName, req.TargetName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "testfiles", "action": "clone", "message": "cloned", "src_name": req.SrcName, "target_name": req.TargetName, "project": req.Project}))
		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "cloned"})
	})
}

func HandleAPITestruns(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")

			var rows []TestRunRow
			var err error

			if name == "" || name == "all" {
				rows, err = store.ListAllRuns(ctx)
			} else {
				rows, err = store.ListRuns(ctx, name, project)
			}

			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			jsonResponse(w, http.StatusOK, map[string]any{
				"status": "finished",
				"token":  "testruns",
				"action": "list",
				"items":  rows,
			})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}

			if err := store.DeleteRunsByTestfile(ctx, name, project); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "testruns", "action": "delete", "testfile": name, "project": project, "message": "all runs deleted"}))
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "all runs deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPITestrun(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			idStr := r.URL.Query().Get("id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}

			row, err := store.GetRun(ctx, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			jsonResponse(w, http.StatusOK, map[string]any{
				"status":          "finished",
				"token":           "testruns",
				"action":          "get",
				"id":              row.ID,
				"testfile":        row.TestfileName,
				"run_number":      row.RunNumber,
				"hash":            row.Hash,
				"result":          row.Status,
				"failure_reason":  row.FailureReason,
				"source":          row.Source,
				"script":          row.Script,
				"batch_id":        row.BatchID,
				"finished_at":     row.FinishedAt,
				"flow_events_b64": base64.StdEncoding.EncodeToString([]byte(row.FlowEvents)),
				"created_at":      row.CreatedAt.Format(time.RFC3339Nano),
			})

		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}

			row, err := store.GetRun(ctx, id)
			if err == nil && (row.Status == "RUNNING" || row.Status == "QUEUED") {
				jsonResponse(w, http.StatusConflict, apiResponse{Status: "error", Message: "cannot delete a running test"})
				return
			}

			if err := store.DeleteRun(ctx, id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			broadcastInfo(hub, statusJSON(map[string]string{"status": "finished", "token": "testruns", "action": "delete", "id": strconv.Itoa(id), "message": "run deleted"}))
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "run deleted"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPITestrunWavs(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		wavs, err := store.ListWavs(ctx, int64(id))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"status": "finished",
			"token":  "testrun_wavs",
			"items":  wavs,
		})
	})
}

func HandleAPITestrunWav(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		filename, path, err := store.GetWavPath(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		file, err := os.Open(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("read wav file: %v", err), http.StatusNotFound)
			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			http.Error(w, fmt.Sprintf("read wav file: %v", err), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, filename, info.ModTime(), file)
	})
}

func HandleAPITestrunDownload(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		downloadType := r.URL.Query().Get("type")

		row, err := store.GetRun(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		events := parseEvents(row.FlowEvents)

		switch downloadType {
		case "flow":
			serveTextDownload(w, fmt.Sprintf("%s_%d_flow.txt", row.TestfileName, id), filterFlowEvents(events))
		case "sip":
			serveTextDownload(w, fmt.Sprintf("%s_%d_sip.txt", row.TestfileName, id), filterTypedEvents(events, "SIP"))
		case "log":
			serveTextDownload(w, fmt.Sprintf("%s_%d_log.txt", row.TestfileName, id), filterTypedEvents(events, "LOG"))
		case "pcap":
			w.Header().Set("Content-Type", "application/vnd.tcpdump.pcap")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s_%d_sip.pcap\"", row.TestfileName, id))
			if _, err := w.Write(generatePcap(events)); err != nil {
				log.Printf("failed to write pcap response: %v", err)
			}
		default:
			http.Error(w, "invalid download type", http.StatusBadRequest)
		}
	})
}

func HandleAPIDatabaseMaintenance(ts *TestStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := ts.Vacuum(r.Context()); err != nil {
			log.Printf("maintenance error: %v", err)
			jsonResponse(w, http.StatusInternalServerError, map[string]any{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}

		jsonResponse(w, http.StatusOK, map[string]any{
			"status": "finished",
		})
	}
}

func HandleAPIProjectRun(hub *WsHub, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if apiKey == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(apiKey)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if hub.testStore == nil {
			http.Error(w, "test store not enabled", http.StatusServiceUnavailable)
			return
		}

		projectName := r.URL.Query().Get("name")
		if projectName == "" {
			http.Error(w, "project name required", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		all, err := hub.testStore.List(ctx, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var batch []TestfileData
		for _, tf := range all {
			if tf.ProjectName == projectName {
				batch = append(batch, TestfileData{Name: tf.Name, ProjectName: tf.ProjectName, Content: tf.Content})
			}
		}

		if len(batch) == 0 {
			http.Error(w, "no testfiles found", http.StatusNotFound)
			return
		}

		batchID, started := hub.runner.StartWithID(batch)
		if !started {
			http.Error(w, "another test run is already active", http.StatusConflict)
			return
		}

		jsonResponse(w, http.StatusAccepted, map[string]any{"status": "started", "batch_id": batchID})
	}
}

func HandleAPICronJobs(hub *WsHub) http.HandlerFunc {
	return withCron(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, cm *CronManager, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			jobs, err := store.ListCronJobs(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{
				"status": "finished",
				"items":  jobs,
			})
		case http.MethodPost:
			var req struct {
				Project  string `json:"project"`
				Testfile string `json:"testfile"`
				CronExpr string `json:"cron_expr"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			req.Project = SanitizeName(req.Project)
			req.Testfile = SanitizeName(req.Testfile)
			if req.Project == "" {
				http.Error(w, "project required", http.StatusBadRequest)
				return
			}

			parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
			if _, err := parser.Parse(req.CronExpr); err != nil {
				http.Error(w, "invalid cron expression: "+err.Error(), http.StatusBadRequest)
				return
			}

			id, err := store.SaveCronJob(ctx, req.Project, req.Testfile, req.CronExpr)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cm.ReloadAll()
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: fmt.Sprintf("job %d saved", id)})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPICronJobModify(hub *WsHub) http.HandlerFunc {
	return withCron(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, cm *CronManager, ctx context.Context) {
		switch r.Method {
		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}
			if err := store.DeleteCronJob(ctx, id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cm.ReloadAll()
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "job deleted"})

		case http.MethodPost:
			var req struct {
				ID     int  `json:"id"`
				Active bool `json:"active"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			if err := store.ToggleCronJob(ctx, req.ID, req.Active); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cm.ReloadAll()
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "job toggled"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPISIPAccounts(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			accounts, err := store.ListSIPAccounts(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Shield passwords from API response
			responseAccounts := make([]map[string]any, len(accounts))
			for i, acc := range accounts {
				hasPassword := acc.Password != ""
				responseAccounts[i] = map[string]any{
					"name":         acc.Name,
					"sip_uri":      acc.SIPURI,
					"uri_params":   SanitizeString(acc.URIParams),
					"addr_params":  SanitizeString(acc.AddrParams),
					"has_password": hasPassword,
					"created_at":   acc.CreatedAt,
					"updated_at":   acc.UpdatedAt,
				}
			}
			jsonResponse(w, http.StatusOK, map[string]any{
				"status": "finished",
				"items":  responseAccounts,
			})
		case http.MethodPost:
			var req struct {
				OldName       string `json:"old_name"`
				Name          string `json:"name"`
				SIPURI        string `json:"sip_uri"`
				Password      string `json:"password"`
				ClearPassword bool   `json:"clear_password"`
				URIParams     string `json:"uri_params"`
				AddrParams    string `json:"addr_params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			name := strings.TrimSpace(req.Name)
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}
			if !isSafeAlias(name) {
				http.Error(w, "invalid account name (only alphanumeric, underscores, and dashes allowed)", http.StatusBadRequest)
				return
			}
			sipURI := strings.TrimSpace(req.SIPURI)
			if sipURI == "" {
				http.Error(w, "sip_uri required", http.StatusBadRequest)
				return
			}

			password := req.Password
			oldName := strings.TrimSpace(req.OldName)
			if req.ClearPassword {
				password = ""
			}
			if password == "" && oldName != "" && !req.ClearPassword {
				if existing, err := store.GetSIPAccount(ctx, oldName); err == nil {
					password = existing.Password
				}
			}

			err := store.SaveSIPAccount(ctx, req.OldName, name, sipURI, password, req.URIParams, req.AddrParams)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: fmt.Sprintf("account %s saved", name)})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPISIPAccountDelete(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name parameter required", http.StatusBadRequest)
			return
		}
		if err := store.DeleteSIPAccount(ctx, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "account deleted"})
	})
}

func HandleAPIWebhooks(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			webhooks, err := store.ListWebhooks(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{
				"status": "finished",
				"items":  webhooks,
			})
		case http.MethodPost:
			var req struct {
				OldAlias string `json:"old_alias"`
				Alias    string `json:"alias"`
				URL      string `json:"url"`
				Enabled  bool   `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			alias := strings.TrimSpace(req.Alias)
			if alias == "" {
				http.Error(w, "alias required", http.StatusBadRequest)
				return
			}
			if !isSafeAlias(alias) {
				http.Error(w, "invalid webhook alias (only alphanumeric, underscores, and dashes allowed)", http.StatusBadRequest)
				return
			}
			url := strings.TrimSpace(req.URL)
			if url == "" {
				http.Error(w, "url required", http.StatusBadRequest)
				return
			}

			err := store.SaveWebhook(ctx, req.OldAlias, alias, url, req.Enabled)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: fmt.Sprintf("webhook %s saved", alias)})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func HandleAPIWebhookDelete(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		alias := r.URL.Query().Get("alias")
		if alias == "" {
			http.Error(w, "alias parameter required", http.StatusBadRequest)
			return
		}
		if err := store.DeleteWebhook(ctx, alias); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "webhook deleted"})
	})
}

func HandleAPILogFilter(hub *WsHub) http.HandlerFunc {
	return withStore(hub, func(w http.ResponseWriter, r *http.Request, store *TestStore, ctx context.Context) {
		switch r.Method {
		case http.MethodGet:
			filters, err := store.GetLogFilters(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResponse(w, http.StatusOK, map[string]any{
				"status":  "finished",
				"filters": filters,
			})

		case http.MethodPost:
			var req struct {
				Filters []string `json:"filters"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			filters := normalizeLogFilters(req.Filters)
			if err := store.SaveLogFilters(ctx, filters); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			hub.SetLogFilters(filters)
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "log filter saved"})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func jsonResponse(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode json response: %v", err)
	}
}
