package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
	"jagocoffee/profiler-service/storage"
)

// Track profiles per window
var (
	profilesMu sync.Mutex
	profiles   = make(map[string][]*Profile) // window -> []*Profile
)

func main() {
	// Init storage
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "profiler.db"
	}
	if err := storage.Init(dbPath); err != nil {
		log.Fatalf("Storage init failed: %v", err)
	}
	defer storage.Close()

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		log.Fatalf("Failed to load timezone: %v", err)
	}

	c := cron.New(cron.WithLocation(loc))

	// Peak hour samples: 3 per window (start, mid, end)
	// Morning: 07:00, 07:30, 08:00
	// Afternoon: 12:00, 12:30, 13:00
	samples := []struct {
		schedule string
		window   string // "morning" or "afternoon"
		sampleNum int   // 1=start, 2=mid, 3=end
	}{
		{"0 7 * * *", "morning", 1},
		{"30 7 * * *", "morning", 2},
		{"0 8 * * *", "morning", 3},
		{"0 12 * * *", "afternoon", 1},
		{"30 12 * * *", "afternoon", 2},
		{"0 13 * * *", "afternoon", 3},
	}

	for _, s := range samples {
		schedule, window, sampleNum := s.schedule, s.window, s.sampleNum
		_, err := c.AddFunc(schedule, func() {
			runProfiler(window, sampleNum)
		})
		if err != nil {
			log.Fatalf("Failed to add cron job %s: %v", schedule, err)
		}
		log.Printf("Registered cron job: %s (%s sample %d/3)", schedule, window, sampleNum)
	}

	c.Start()
	log.Println("Scheduler started. Press Ctrl+C to stop.")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutdown signal received. Stopping scheduler...")
	ctx := c.Stop()
	<-ctx.Done()
	log.Println("Scheduler stopped gracefully.")
}

func runProfiler(window string, sampleNum int) {
	now := time.Now()
	fmt.Printf("[%s] Profiler triggered - %s peak, sample %d/3\n",
		now.Format("2006-01-02 15:04:05"), window, sampleNum)

	prof, err := fetchProfile(window, sampleNum)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to fetch profile (sample %d): %v", sampleNum, err)
		log.Printf("Error: %s", errMsg)
		sendSlackError(window, errMsg)
		return
	}

	log.Printf("Profile fetched: %s (sample %d/3)", window, sampleNum)

	// Collect profile
	profilesMu.Lock()
	profiles[window] = append(profiles[window], prof)
	profilesMu.Unlock()

	// Only analyze on end sample (sample 3)
	if sampleNum == 3 {
		log.Printf("End sample collected for %s. Analyzing...", window)
		go analyzeAndReport(window)
	}
}

func analyzeAndReport(window string) {
	profilesMu.Lock()
	profs := profiles[window]
	profilesMu.Unlock()

	if len(profs) != 3 {
		errMsg := fmt.Sprintf("Expected 3 profiles, got %d", len(profs))
		log.Printf("Error: %s", errMsg)
		sendSlackError(window, errMsg)
		return
	}

	// Analyze
	result, err := analyzeProfiles(window, profs)
	if err != nil {
		errMsg := fmt.Sprintf("Analysis failed: %v", err)
		log.Printf("Error: %s", errMsg)
		sendSlackError(window, errMsg)
		return
	}

	// Save to DB
	anomaliesJSON, _ := json.Marshal(result.Anomalies)
	metricsJSON, _ := json.Marshal(result.Metrics)
	ydayJSON, _ := json.Marshal(result.YdayComparison)

	rec := &storage.ProfileRecord{
		RunTimestamp:   profs[0].Timestamp,
		Window:         window,
		SampleNum:      3,
		CPUProfileURL:  profs[0].CPUPath,
		HeapProfileURL: profs[0].HeapPath,
		CPUTextURL:     profs[0].CPUTextPath,
		HeapTextURL:    profs[0].HeapTextPath,
		Summary:        result.Summary,
		Anomalies:      string(anomaliesJSON),
		Metrics:        string(metricsJSON),
		YdayComparison: string(ydayJSON),
	}

	if err := storage.SaveProfile(rec); err != nil {
		errMsg := fmt.Sprintf("DB save failed: %v", err)
		log.Printf("Error: %s", errMsg)
		sendSlackError(window, errMsg)
		return
	}

	log.Printf("Analysis complete for %s. Saved to DB.", window)

	// Clear from map
	profilesMu.Lock()
	delete(profiles, window)
	profilesMu.Unlock()

	// Send report to Slack
	sendSlackReport(result, window)

	// TODO: Prompt GitHub issues
}
