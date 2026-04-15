package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"jagocoffee/profiler-service/storage"
)

func TestIntegration(t *testing.T) {
	// Setup
	dbPath := "test_profiler.db"
	defer os.Remove(dbPath)

	if err := storage.Init(dbPath); err != nil {
		t.Fatalf("Storage init failed: %v", err)
	}
	defer storage.Close()

	// Create fake profiles
	now := time.Now()
	fakeProfiles := createFakeProfiles("morning", now)

	// Create fake analysis result
	result := &AnalysisResult{
		Summary: "CPU stable at 45%. Heap +2% vs yesterday. No anomalies.",
		Anomalies: []Anomaly{
			{
				Name:      "memory_usage",
				Severity:  "medium",
				Details:   "Heap growth slightly above normal",
				Variance:  3.2,
			},
		},
		Metrics: map[string]interface{}{
			"peak_cpu":    45.2,
			"peak_heap":   512,
			"avg_cpu":     38.5,
			"avg_heap":    480,
		},
		YdayComparison: map[string]interface{}{
			"cpu_variance":  2.1,
			"heap_variance": 3.2,
			"status":        "normal",
		},
	}

	// Save to DB
	anomaliesJSON, _ := json.Marshal(result.Anomalies)
	metricsJSON, _ := json.Marshal(result.Metrics)
	ydayJSON, _ := json.Marshal(result.YdayComparison)

	rec := &storage.ProfileRecord{
		RunTimestamp:   now,
		Window:         "morning",
		SampleNum:      3,
		CPUProfileURL:  fakeProfiles[0].CPUPath,
		HeapProfileURL: fakeProfiles[0].HeapPath,
		CPUTextURL:     fakeProfiles[0].CPUTextPath,
		HeapTextURL:    fakeProfiles[0].HeapTextPath,
		Summary:        result.Summary,
		Anomalies:      string(anomaliesJSON),
		Metrics:        string(metricsJSON),
		YdayComparison: string(ydayJSON),
	}

	err := storage.SaveProfile(rec)
	if err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}
	fmt.Println("✓ Saved profile to DB")

	// Query back
	retrieved, err := storage.GetProfileByTimestamp(now)
	if err != nil {
		t.Fatalf("GetProfileByTimestamp failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("Retrieved profile is nil")
	}

	if retrieved.Summary != result.Summary {
		t.Errorf("Summary mismatch. Got: %s, Want: %s", retrieved.Summary, result.Summary)
	}
	fmt.Println("✓ Retrieved profile from DB")

	// Parse anomalies
	var anomalies []Anomaly
	if err := json.Unmarshal([]byte(retrieved.Anomalies), &anomalies); err != nil {
		t.Fatalf("Parse anomalies failed: %v", err)
	}

	if len(anomalies) != 1 {
		t.Errorf("Expected 1 anomaly, got %d", len(anomalies))
	}
	if anomalies[0].Name != "memory_usage" {
		t.Errorf("Anomaly name mismatch. Got: %s", anomalies[0].Name)
	}
	fmt.Println("✓ Anomalies parsed correctly")

	// Parse metrics
	var metrics map[string]interface{}
	if err := json.Unmarshal([]byte(retrieved.Metrics), &metrics); err != nil {
		t.Fatalf("Parse metrics failed: %v", err)
	}

	if metrics["peak_cpu"] != 45.2 {
		t.Errorf("Peak CPU mismatch. Got: %v", metrics["peak_cpu"])
	}
	fmt.Println("✓ Metrics parsed correctly")

	fmt.Println("\n✅ Integration test passed!")
}

func createFakeProfiles(window string, timestamp time.Time) []*Profile {
	profiles := make([]*Profile, 3)

	cpuText := `==Sample CPU Profile==
Showing nodes accounting for 450MB, 90% of 500MB total
      flat  flat%   sum%        cum   cum%
      150MB 30.0% 30.0%       200MB 40.0%  runtime.mallocgc
      100MB 20.0% 50.0%       100MB 20.0%  encoding/json.Unmarshal
       80MB 16.0% 66.0%       80MB 16.0%  sync.(*Mutex).Lock
`

	heapText := `==Sample Heap Profile==
Showing nodes accounting for 512MB, 100% of 512MB total
      flat  flat%   sum%        cum   cum%
      200MB 39.1% 39.1%       200MB 39.1%  sync.(*Mutex).Lock
      150MB 29.3% 68.4%       150MB 29.3%  runtime.newobject
      100MB 19.5% 87.9%       100MB 19.5%  encoding/json.Unmarshal
       62MB 12.1%100.0%        62MB 12.1%  other
`

	for i := 0; i < 3; i++ {
		prof := &Profile{
			Window:    window,
			SampleNum: i + 1,
			Timestamp: timestamp,
			CPUText:   cpuText,
			HeapText:  heapText,
		}
		// Set dummy paths
		prof.CPUPath = fmt.Sprintf("profiles/%s-%d-cpu.prof", window, i)
		prof.HeapPath = fmt.Sprintf("profiles/%s-%d-heap.prof", window, i)
		prof.CPUTextPath = fmt.Sprintf("profiles/%s-%d-cpu.txt", window, i)
		prof.HeapTextPath = fmt.Sprintf("profiles/%s-%d-heap.txt", window, i)

		profiles[i] = prof
	}

	return profiles
}
