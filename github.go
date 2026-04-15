package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func uploadProfilesToGitHub(profiles []*Profile, window string) (map[string]string, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Println("Warning: GITHUB_TOKEN not set, skipping GitHub upload")
		return nil, nil
	}

	repo := "jagocoffee/profiler-service"
	urls := make(map[string]string)

	// Upload each profile (CPU text, heap text)
	for i, prof := range profiles {
		// Upload CPU text
		cpuPath := fmt.Sprintf("profiler-service/profiles/%s-%d-cpu.txt",
			prof.Timestamp.Format("2006-01-02-1504"), i+1)
		cpuURL, err := uploadFile(token, repo, cpuPath, prof.CPUText,
			fmt.Sprintf("Add CPU profile for %s sample %d", window, i+1))
		if err != nil {
			log.Printf("Warning: upload CPU failed: %v", err)
		} else {
			urls[fmt.Sprintf("cpu_%d", i+1)] = cpuURL
			log.Printf("Uploaded: %s", cpuPath)
		}

		// Upload heap text
		heapPath := fmt.Sprintf("profiler-service/profiles/%s-%d-heap.txt",
			prof.Timestamp.Format("2006-01-02-1504"), i+1)
		heapURL, err := uploadFile(token, repo, heapPath, prof.HeapText,
			fmt.Sprintf("Add heap profile for %s sample %d", window, i+1))
		if err != nil {
			log.Printf("Warning: upload heap failed: %v", err)
		} else {
			urls[fmt.Sprintf("heap_%d", i+1)] = heapURL
			log.Printf("Uploaded: %s", heapPath)
		}
	}

	return urls, nil
}

func uploadFile(token, repo, path, content, message string) (string, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	reqBody := map[string]interface{}{
		"message": message,
		"content": encoded,
		"branch":  "main",
	}

	bodyBytes, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", repo, path)

	req, _ := http.NewRequest("PUT", url, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response to get file URL
	var respData map[string]interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		return "", err
	}

	// Try to get html_url from content
	if content, ok := respData["content"].(map[string]interface{}); ok {
		if htmlURL, ok := content["html_url"].(string); ok {
			return htmlURL, nil
		}
	}

	// Fallback to construct URL
	return fmt.Sprintf("https://github.com/%s/blob/main/%s", repo, path), nil
}
