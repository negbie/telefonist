package main

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const warnEmo = ":warning: "
const fireEmo = ":fire: "

func page(webhookUrl string, level, msg string) error {
	if level == "warning" {
		msg = warnEmo + msg
	} else if level == "error" {
		msg = fireEmo + msg
	}

	text := "{\"text\":" + fmt.Sprintf("%q", msg) + ",\"username\":\"telefonist\"}"
	req, err := http.NewRequest(http.MethodPost, webhookUrl, strings.NewReader(text))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if buf.String() != "ok" {
		return fmt.Errorf("webhook returned HTTP status %s (%d)", resp.Status, resp.StatusCode)
	}
	return nil
}
