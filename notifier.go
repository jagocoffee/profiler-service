package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func sendSlackError(window string, errMsg string) {
	token := os.Getenv("SLACK_BOT_TOKEN")
	channel := os.Getenv("SLACK_CHANNEL_ID")

	if token == "" || channel == "" {
		log.Printf("Slack credentials not set. Error: %s", errMsg)
		return
	}

	msg := map[string]interface{}{
		"channel": channel,
		"text":    fmt.Sprintf("❌ Profiler Error - %s peak\n%s", window, errMsg),
		"blocks": []map[string]interface{}{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("❌ *Profiler Error* - %s peak\n```%s```", window, errMsg),
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(msg)
	req, _ := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Slack send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Slack API error: status %d", resp.StatusCode)
	}
}

func sendSlackReport(result *AnalysisResult, window string) {
	token := os.Getenv("SLACK_BOT_TOKEN")
	channel := os.Getenv("SLACK_CHANNEL_ID")

	if token == "" || channel == "" {
		log.Printf("Slack credentials not set. Skipping report.")
		return
	}

	anomalyText := "None"
	if len(result.Anomalies) > 0 {
		anomalyText = ""
		for _, a := range result.Anomalies {
			anomalyText += fmt.Sprintf("• %s (%s, %.1f%% variance)\n", a.Name, a.Severity, a.Variance)
		}
	}

	msg := map[string]interface{}{
		"channel": channel,
		"text":    fmt.Sprintf("📊 Profiler Report - %s", window),
		"blocks": []map[string]interface{}{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("📊 *Profiler Report* - %s peak\n\n*Summary:*\n%s\n\n*Anomalies:*\n%s", window, result.Summary, anomalyText),
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(msg)
	req, _ := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Slack send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Printf("Slack report sent for %s", window)
	}
}
