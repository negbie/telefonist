package telefonist

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)


var webhookClient = &http.Client{
	Timeout: 5 * time.Second,
}

func postWebhook(webhookUrl string, payload string) error {
	req, err := http.NewRequest(http.MethodPost, webhookUrl, strings.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := webhookClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			log.Printf("failed to read webhook error response body: %v", readErr)
		}
		return fmt.Errorf("webhook returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, string(body))
	}

	// Drain any remaining body to allow connection reuse
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		log.Printf("failed to drain webhook response body: %v", err)
	}
	return nil
}
