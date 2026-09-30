package telefonist

import (
	"context"
	"encoding/xml"
	"net/http"
)

func HandleRunner(h *WsHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		jsonResponse(w, 200, h.runner.Snapshot())
	}
}

func batchRuns(ctx context.Context, s *TestStore, id string) ([]TestRunRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM testruns WHERE batch_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]TestRunRow, 0, len(ids))
	for _, id := range ids {
		run, err := s.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		run.FlowEvents = ""
		run.Source = ""
		run.Script = ""
		out = append(out, run)
	}
	return out, nil
}

func HandleBatch(h *WsHub) http.HandlerFunc {
	return withStore(h, func(w http.ResponseWriter, r *http.Request, s *TestStore, ctx context.Context) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "batch ID required", 400)
			return
		}
		state := h.runner.Snapshot()
		if r.Method == http.MethodDelete {
			if !h.runner.StopBatch(id) {
				http.Error(w, "batch is not active", 409)
				return
			}
			jsonResponse(w, 202, map[string]string{"status": "stopping"})
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		runs, err := batchRuns(ctx, s, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if len(runs) == 0 && state.BatchID != id {
			http.Error(w, "batch not found", 404)
			return
		}
		jsonResponse(w, 200, map[string]any{"batch_id": id, "active": state.Active && state.BatchID == id, "runs": runs})
	})
}

type junitCase struct {
	Name    string  `xml:"name,attr"`
	Class   string  `xml:"classname,attr"`
	Failure *string `xml:"failure,omitempty"`
	Error   *string `xml:"error,omitempty"`
	Skipped *string `xml:"skipped,omitempty"`
}
type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Errors   int         `xml:"errors,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Cases    []junitCase `xml:"testcase"`
}

func HandleBatchJUnit(h *WsHub) http.HandlerFunc {
	return withStore(h, func(w http.ResponseWriter, r *http.Request, s *TestStore, ctx context.Context) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "batch ID required", 400)
			return
		}
		state := h.runner.Snapshot()
		if state.Active && state.BatchID == id {
			http.Error(w, "batch still running", 409)
			return
		}
		runs, err := batchRuns(ctx, s, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if len(runs) == 0 {
			http.Error(w, "batch not found", 404)
			return
		}
		suite := junitSuite{Tests: len(runs)}
		for _, run := range runs {
			c := junitCase{Name: run.TestfileName, Class: run.ProjectName}
			reason := run.FailureReason
			if reason == "" {
				reason = run.Status
			}
			switch run.Status {
			case "PASS":
			case "FAIL":
				suite.Failures++
				c.Failure = &reason
			case "SKIPPED", "UNVERIFIED":
				suite.Skipped++
				c.Skipped = &reason
			default:
				suite.Errors++
				c.Error = &reason
			}
			suite.Cases = append(suite.Cases, c)
		}
		w.Header().Set("Content-Type", "application/xml")
		_ = xml.NewEncoder(w).Encode(suite)
	})
}
