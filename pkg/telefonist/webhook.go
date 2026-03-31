package telefonist

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func sendResultWebhook(webhookUrl string, fileName, projectName string, total int, expectedHash, actualHash, result string, runID int64) error {
	color := "008000" // Green for PASS
	if result != "PASS" {
		color = "FF0000" // Red for FAIL
	}

	msg := fmt.Sprintf("Status: finished\nToken: testfile\nFile: %s\nProject: %s\nTotal: %d\nExpected_hash: %s\nActual_hash: %s\nResult: %s\nRun_id: %d",
		fileName, projectName, total, expectedHash, actualHash, result, runID)

	// Use MessageCard format for Teams styling support
	payload := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": color,
		"summary":    fmt.Sprintf("Test Run %s: %s", fileName, result),
		"sections": []map[string]interface{}{
			{
				"activityTitle": fmt.Sprintf("Test Run: **%s**", fileName),
				"text":          strings.ReplaceAll(msg, "\n", "  \n"), // Teams uses 2 spaces for newline in Markdown
				"markdown":      true,
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return postWebhook(webhookUrl, string(data))
}

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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webhook returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, string(body))
	}

	// Drain any remaining body to allow connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
