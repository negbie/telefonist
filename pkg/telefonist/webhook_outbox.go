package telefonist

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"time"
)

func enqueueResultWebhook(s *TestStore, address, file, project, hash, status string, id int64) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		log.Printf("invalid webhook destination for run %d", id)
		return
	}
	text := fmt.Sprintf("%s / %s: %s (run %d, hash %s)", project, file, status, id, hash)
	payload, _ := json.Marshal(map[string]any{"@type": "MessageCard", "@context": "http://schema.org/extensions", "summary": text, "text": text})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO webhook_outbox(url,payload,next_attempt) VALUES(?,?,?)`, address, string(payload), time.Now().Format(time.RFC3339Nano)); err != nil {
		log.Printf("cannot queue webhook for run %d: %v", id, err)
	}
}

func runWebhookWorker(ctx context.Context, s *TestStore) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var id, attempts int
				var address, payload string
				if err := s.db.QueryRowContext(ctx, `SELECT id,url,payload,attempts FROM webhook_outbox WHERE next_attempt<=? ORDER BY id LIMIT 1`, time.Now().Format(time.RFC3339Nano)).Scan(&id, &address, &payload, &attempts); err != nil {
					continue
				}
				if err := postWebhook(address, payload); err == nil {
					_, _ = s.db.ExecContext(ctx, `DELETE FROM webhook_outbox WHERE id=?`, id)
				} else {
					attempts++
					delay := time.Duration(min(attempts*attempts, 360)) * 10 * time.Second
					_, _ = s.db.ExecContext(ctx, `UPDATE webhook_outbox SET attempts=?,last_error=?,next_attempt=? WHERE id=?`, attempts, "delivery failed; inspect destination", time.Now().Add(delay).Format(time.RFC3339Nano), id)
				}
			}
		}
	}()
	return done
}
