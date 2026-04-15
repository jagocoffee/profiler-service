package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"jagocoffee/profiler-service/storage"
)

type AnalysisResult struct {
	Summary        string                 `json:"summary"`
	Anomalies      []Anomaly              `json:"anomalies"`
	Metrics        map[string]interface{} `json:"metrics"`
	YdayComparison map[string]interface{} `json:"yday_comparison"`
}

type Anomaly struct {
	Name     string  `json:"name"`
	Severity string  `json:"severity"` // low, medium, high
	Details  string  `json:"details"`
	Variance float64 `json:"variance_pct"`
}

func analyzeProfiles(window string, profiles []*Profile) (*AnalysisResult, error) {
	if len(profiles) != 3 {
		return nil, fmt.Errorf("expected 3 profiles, got %d", len(profiles))
	}

	// Get yesterday's profile for comparison
	ydayProf, err := storage.GetProfileByTimestamp(profiles[0].Timestamp.AddDate(0, 0, -1))
	if err != nil {
		log.Printf("Warning: couldn't fetch yesterday profile: %v", err)
	}

	// Build prompt
	prompt := buildPrompt(window, profiles, ydayProf)

	// Call Claude Opus
	result, err := callOpusAPI(prompt)
	if err != nil {
		return nil, fmt.Errorf("opus call failed: %w", err)
	}

	return result, nil
}

func buildPrompt(window string, profiles []*Profile, ydayProf *storage.ProfileRecord) string {
	prompt := fmt.Sprintf(`Analyze profiler data for %s peak hours (UTC+7).

Today's profiles (3 samples: start, mid, end):

=== 07:00 CPU Profile ===
%s

=== 07:00 Heap Profile ===
%s

=== 07:30 CPU Profile ===
%s

=== 07:30 Heap Profile ===
%s

=== 08:00 CPU Profile ===
%s

=== 08:00 Heap Profile ===
%s
`, window, profiles[0].CPUText, profiles[0].HeapText, profiles[1].CPUText, profiles[1].HeapText, profiles[2].CPUText, profiles[2].HeapText)

	if ydayProf != nil {
		prompt += fmt.Sprintf(`

Yesterday's summary:
%s

Anomalies from yesterday:
%s
`, ydayProf.Summary, ydayProf.Anomalies)
	}

	prompt += `

Task:
1. Summarize CPU and heap usage (1 paragraph)
2. Compare with yesterday (if available)
3. Flag any anomalies with >5% variance from yesterday
4. Provide metrics (peak CPU, peak heap, avg, etc)

Format response as JSON:
{
  "summary": "...",
  "anomalies": [
    {"name": "...", "severity": "low|medium|high", "details": "...", "variance_pct": X.X}
  ],
  "metrics": {"peak_cpu": X, "peak_heap": X, ...},
  "yday_comparison": {"variance_cpu_pct": X, "variance_heap_pct": X, ...}
}
`

	return prompt
}

func callOpusAPI(prompt string) (*AnalysisResult, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	reqBody := map[string]interface{}{
		"model":      "claude-opus-4-1-20250805",
		"max_tokens": 2000,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, string(body))
	}

	var respBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Extract text from response
	content, ok := respBody["content"].([]interface{})
	if !ok || len(content) == 0 {
		return nil, fmt.Errorf("invalid response format")
	}

	contentBlock := content[0].(map[string]interface{})
	responseText := contentBlock["text"].(string)

	// Parse JSON from response
	result := &AnalysisResult{}
	if err := parseOpusResponse(responseText, result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return result, nil
}

func parseOpusResponse(text string, result *AnalysisResult) error {
	// Extract JSON from response (may have markdown fences)
	jsonStart := strings.Index(text, "{")
	jsonEnd := strings.LastIndex(text, "}") + 1

	if jsonStart == -1 || jsonEnd <= 0 {
		return fmt.Errorf("no JSON found in response")
	}

	jsonStr := text[jsonStart:jsonEnd]
	if err := json.Unmarshal([]byte(jsonStr), result); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	return nil
}
