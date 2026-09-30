package telefonist

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

func HandleOperations(h *WsHub) http.HandlerFunc {
	return withStore(h, func(w http.ResponseWriter, r *http.Request, s *TestStore, ctx context.Context) {
		if r.Method == http.MethodPost {
			var req struct {
				Retention int `json:"retention_runs"`
			}
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.Retention < 1 || req.Retention > 10000 {
				http.Error(w, "retention_runs must be 1–10000", 400)
				return
			}
			if err := s.SaveSetting(ctx, "retention_runs", strconv.Itoa(req.Retention)); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		keep := 100
		if v, e := s.GetSetting(ctx, "retention_runs"); e == nil {
			if n, e := strconv.Atoi(v); e == nil && n > 0 {
				keep = n
			}
		}
		var pending, failed, cleanup int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN attempts>0 THEN 1 ELSE 0 END),0) FROM webhook_outbox`).Scan(&pending, &failed); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifact_deletions`).Scan(&cleanup); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		schedules := map[int]any{}
		if cm := h.cronManager; cm != nil {
			cm.mu.Lock()
			for id, entry := range cm.entryIDs {
				e := cm.c.Entry(entry)
				schedules[id] = map[string]any{"next": e.Next, "previous": e.Prev}
			}
			cm.mu.Unlock()
		}
		jsonResponse(w, 200, map[string]any{"retention_runs": keep, "pending_webhooks": pending, "retrying_webhooks": failed, "pending_artifact_deletions": cleanup, "schedules": schedules, "schedule_timezone": "UTC (override with CRON_TZ=Zone in an expression)"})
	})
}
