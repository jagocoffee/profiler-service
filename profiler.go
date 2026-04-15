package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const (
	profileDuration = 30
)

var jaguServiceURL string

func init() {
	jaguServiceURL = os.Getenv("JAGO_SERVICE_URL")
	if jaguServiceURL == "" {
		jaguServiceURL = "https://jago-service.jagocoffee.dev"
	}
}

type Profile struct {
	Window      string    // "morning" or "afternoon"
	SampleNum   int       // 1, 2, or 3
	Timestamp   time.Time
	CPUProf     []byte
	HeapProf    []byte
	CPUText     string
	HeapText    string
	CPUPath     string // local file path
	HeapPath    string
	CPUTextPath string
	HeapTextPath string
}

func fetchProfile(window string, sampleNum int) (*Profile, error) {
	now := time.Now()
	prof := &Profile{
		Window:    window,
		SampleNum: sampleNum,
		Timestamp: now,
	}

	// Create profiles/ directory if not exists
	if err := os.MkdirAll("profiles", 0755); err != nil {
		return nil, fmt.Errorf("mkdir profiles: %w", err)
	}

	// File naming: profiles/2026-04-15-0700-morning-1-cpu.prof
	baseName := fmt.Sprintf("profiles/%s-%s-%d",
		now.Format("2006-01-02-1504"), window, sampleNum)

	prof.CPUPath = baseName + "-cpu.prof"
	prof.HeapPath = baseName + "-heap.prof"
	prof.CPUTextPath = baseName + "-cpu.txt"
	prof.HeapTextPath = baseName + "-heap.txt"

	// Fetch CPU profile
	log.Printf("[%s] Fetching CPU profile...", now.Format("15:04:05"))
	cpuData, err := httpGet(fmt.Sprintf("%s/debug/pprof/profile?seconds=%d", jaguServiceURL, profileDuration))
	if err != nil {
		return nil, fmt.Errorf("fetch CPU profile: %w", err)
	}
	prof.CPUProf = cpuData

	// Fetch heap profile
	log.Printf("[%s] Fetching heap profile...", now.Format("15:04:05"))
	heapData, err := httpGet(fmt.Sprintf("%s/debug/pprof/heap", jaguServiceURL))
	if err != nil {
		return nil, fmt.Errorf("fetch heap profile: %w", err)
	}
	prof.HeapProf = heapData

	// Save raw profiles
	if err := os.WriteFile(prof.CPUPath, prof.CPUProf, 0644); err != nil {
		return nil, fmt.Errorf("write CPU prof: %w", err)
	}
	if err := os.WriteFile(prof.HeapPath, prof.HeapProf, 0644); err != nil {
		return nil, fmt.Errorf("write heap prof: %w", err)
	}
	log.Printf("Saved: %s, %s", prof.CPUPath, prof.HeapPath)

	// Convert to text
	log.Printf("[%s] Converting profiles to text...", now.Format("15:04:05"))
	cpuText, err := convertProfToText(prof.CPUPath)
	if err != nil {
		return nil, fmt.Errorf("convert CPU prof: %w", err)
	}
	prof.CPUText = cpuText

	heapText, err := convertProfToText(prof.HeapPath)
	if err != nil {
		return nil, fmt.Errorf("convert heap prof: %w", err)
	}
	prof.HeapText = heapText

	// Save text versions
	if err := os.WriteFile(prof.CPUTextPath, []byte(prof.CPUText), 0644); err != nil {
		return nil, fmt.Errorf("write CPU text: %w", err)
	}
	if err := os.WriteFile(prof.HeapTextPath, []byte(prof.HeapText), 0644); err != nil {
		return nil, fmt.Errorf("write heap text: %w", err)
	}
	log.Printf("Converted: %s, %s", prof.CPUTextPath, prof.HeapTextPath)

	return prof, nil
}

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "profiler-service")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

func convertProfToText(profPath string) (string, error) {
	cmd := exec.Command("go", "tool", "pprof", "-text", profPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pprof cmd: %w", err)
	}

	return out.String(), nil
}
