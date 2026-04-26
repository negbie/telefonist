package telefonist

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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

// withStore wraps a handler to check testStore availability and create a 5s timeout context.
func withStore(hub *WsHub, fn storeHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "save", "message": "saved", "name": req.Name}))
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "saved"})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}
			if err := store.DeleteProject(ctx, name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "delete", "message": "deleted", "name": name}))
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

		if err := store.RenameProject(ctx, req.OldName, req.NewName); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "rename", "message": "renamed", "old_name": req.OldName, "new_name": req.NewName}))
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

		hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "projects", "action": "clone", "message": "cloned", "src_name": req.SrcName, "target_name": req.TargetName}))
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

			req.Name = SanitizeName(req.Name)
			req.Project = SanitizeName(req.Project)

			decoded, err := base64.StdEncoding.DecodeString(req.Content)
			if err != nil {
				http.Error(w, "invalid base64", http.StatusBadRequest)
				return
			}

			if err := store.Save(ctx, req.Name, req.Project, string(decoded)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "testfiles", "action": "save", "message": "saved", "name": req.Name, "project": req.Project}))
			jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "saved"})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			project := r.URL.Query().Get("project")
			if name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}

			if err := store.Delete(ctx, name, project); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "testfiles", "action": "delete", "message": "deleted", "name": name, "project": project}))
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

		if err := store.Rename(ctx, req.OldName, req.OldProject, req.NewName, req.NewProject); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "testfiles", "action": "rename", "message": "renamed", "old_name": req.OldName, "old_project": req.OldProject, "new_name": req.NewName, "new_project": req.NewProject}))
		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "renamed"})
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

			hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "testruns", "action": "delete", "testfile": name, "project": project, "message": "all runs deleted"}))
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
				"flow_events_b64": base64.StdEncoding.EncodeToString([]byte(row.FlowEvents)),
				"created_at":      row.CreatedAt.UTC().Format(time.RFC3339Nano),
			})

		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			id, err := strconv.Atoi(idStr)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}

			if err := store.DeleteRun(ctx, id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			hub.broadcast <- []byte(statusJSON(map[string]string{"status": "finished", "token": "testruns", "action": "delete", "id": strconv.Itoa(id), "message": "run deleted"}))
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

		filename, content, err := store.GetWav(ctx, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if _, err := w.Write(content); err != nil {
			log.Printf("failed to write wav response: %v", err)
		}
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

		if r.Header.Get("X-API-Key") != apiKey {
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

		if !runTestfilesBatch(hub, batch) {
			http.Error(w, "another test run is already active", http.StatusConflict)
			return
		}

		jsonResponse(w, http.StatusOK, apiResponse{Status: "finished", Message: "started"})
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

func jsonResponse(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode json response: %v", err)
	}
}
