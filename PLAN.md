# Jago Profiler Service - Architecture Plan

**Status:** Planning  
**Date:** 2026-04-15  
**Timezone:** UTC+7  

---

## Overview

Daily automated profiler for jago-service. Runs CPU + RAM profiles at peak hours (07:00-08:00, 12:00-13:00 UTC+7). Analyzes with Claude Opus, compares against yesterday's data, sends summary to Slack, prompts user to create GitHub issues for anomalies.

**Platform:** Railway (new service)  
**Language:** Go  
**Storage:** GitHub repo (profiles) + SQLite (metadata)  
**External APIs:** Claude Opus, Slack, GitHub  
**Anomaly threshold:** 5% variance vs yesterday

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ Railway Scheduled Job (Go Service)                      │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌─────────────────────────────────────────────────┐  │
│  │ Scheduler (robfig/cron)                         │  │
│  │ - Fires at 07:00, 08:00, 12:00, 13:00 UTC+7    │  │
│  └──────────────────┬──────────────────────────────┘  │
│                     │                                  │
│  ┌──────────────────▼──────────────────────────────┐  │
│  │ Profiler (pprof fetch)                          │  │
│  │ - HTTP GET jago-service LB endpoint             │  │
│  │ - /debug/pprof/profile?seconds=30 (CPU)        │  │
│  │ - /debug/pprof/heap (RAM)                       │  │
│  │ - Save raw .prof files                          │  │
│  └──────────────────┬──────────────────────────────┘  │
│                     │                                  │
│  ┌──────────────────▼──────────────────────────────┐  │
│  │ Analyzer (Claude Opus API)                      │  │
│  │ - Read profiles (text format via go tool pprof) │  │
│  │ - Fetch yesterday's metadata from SQLite        │  │
│  │ - Send to Opus: "Analyze + compare yday"       │  │
│  │ - Extract anomalies (>5% variance)              │  │
│  └──────────────────┬──────────────────────────────┘  │
│                     │                                  │
│  ┌──────────────────▼──────────────────────────────┐  │
│  │ Notifier                                        │  │
│  │ - Store results in SQLite                       │  │
│  │ - Commit profiles to GitHub                     │  │
│  │ - Send summary to Slack                         │  │
│  │ - If anomalies: prompt user GitHub issue link  │  │
│  └──────────────────────────────────────────────────┘  │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

---

## Service Components

### 1. Scheduler (`scheduler.go`)
```
- robfig/cron with UTC+7 timezone
- Cron expressions:
  - 0 7 * * * (07:00 daily)
  - 0 8 * * * (08:00 daily)
  - 0 12 * * * (12:00 daily)
  - 0 13 * * * (13:00 daily)
- Trigger profiler pipeline
```

### 2. Profiler (`profiler.go`)
```
- HTTP client to jago-service load balancer
- Endpoints:
  - GET https://jago-service.jagocoffee.dev/debug/pprof/profile?seconds=30
  - GET https://jago-service.jagocoffee.dev/debug/pprof/heap
- Convert binary profiles to text:
  - `go tool pprof -text <cpu.prof>`
  - `go tool pprof -text <heap.prof>`
- Store raw + text outputs locally/GitHub
```

### 3. Analyzer (`analyzer.go`)
```
- Read profile text output
- Query yesterday's data from SQLite (timestamp, summary, peak metrics)
- Format prompt for Claude:
  ```
  Today's CPU Profile:
  [profile text]
  
  Yesterday's summary:
  [yday metadata]
  
  Analyze and compare. Flag anomalies (>5% variance, new hotspots).
  ```
- Call Claude Opus API
- Parse response for:
  - Summary (1 paragraph)
  - Anomalies (list, >5% variance threshold)
  - Recommended action (if any)
```

### 4. Notifier (`notifier.go`)
```
- Save to SQLite:
  - timestamp, cpu_profile_url, heap_profile_url
  - summary (from Opus)
  - anomalies (JSON)
  - metrics (peak CPU, peak heap)
- Push to GitHub:
  - Commit profiles/ directory
  - Tag with date (e.g., profiles/2026-04-15-0700.prof)
- Send Slack:
  - Title: "Profiler Report - [date] [time]"
  - Summary paragraph
  - Anomalies (if any, >5% threshold)
  - GitHub issue prompt: "Issues to open?"
    - [High CPU hotspot](https://github.com/jagocoffee/jago-service/issues/new?title=CPU+hotspot+in+X&body=...)
    - [Memory regression](https://github.com/jagocoffee/jago-service/issues/new?title=Memory+regression&body=...)
```

---

## Code Structure

```
profiler-service/
├── main.go               # Entry, scheduler init
├── scheduler.go          # cron setup, peak hours
├── profiler.go           # pprof fetch + convert
├── analyzer.go           # Claude Opus integration
├── notifier.go           # Slack + GitHub + DB
├── models/
│   └── profile.go        # structs: Profile, Anomaly, etc
├── storage/
│   └── sqlite.go         # SQLite client (save + fetch yday)
├── slack/
│   └── client.go         # Slack API wrapper
├── config/
│   └── config.go         # env vars + defaults
├── go.mod
├── go.sum
├── Dockerfile
├── railway.toml
└── PLAN.md
```

---

## Database Schema (SQLite)

```sql
CREATE TABLE profiles (
  id INTEGER PRIMARY KEY,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  run_timestamp TIMESTAMP NOT NULL,  -- when profile was taken
  
  cpu_profile_url VARCHAR(500),      -- GitHub raw URL
  heap_profile_url VARCHAR(500),
  
  summary TEXT NOT NULL,              -- Opus analysis (1 paragraph)
  anomalies TEXT DEFAULT '[]',        -- JSON: [{name, severity, details}]
  
  metrics TEXT NOT NULL,              -- JSON: {peak_cpu, peak_heap, duration}
  
  yday_comparison TEXT,               -- JSON: {yday_summary, variance%, new_hotspots}
  
  UNIQUE(run_timestamp)
);

CREATE INDEX idx_run_timestamp ON profiles(run_timestamp);

-- Example anomaly:
{
  "name": "cpu_hotspot_sync",
  "severity": "high",
  "details": "Mutex contention in syncPool doubled vs yday",
  "location": "internal/cache/sync.go:123",
  "variance_pct": 12.5
}
```

---

## Environment Variables

```bash
# Railway
RAILWAY_ENV=production

# Service
JAGO_SERVICE_URL=https://jago-service.jagocoffee.dev
PROFILE_DURATION_SECONDS=30

# Slack
SLACK_BOT_TOKEN=xoxb-...
SLACK_CHANNEL_ID=C...          # Provide later

# GitHub
GITHUB_TOKEN=ghp_...           # Railway secret
GITHUB_REPO=jagocoffee/jago-service

# Claude API
ANTHROPIC_API_KEY=sk-ant-...   # Railway secret
ANTHROPIC_MODEL=claude-opus-4-1-20250805

# Timezone
TZ=Asia/Jakarta  # UTC+7
```

---

## Incremental Implementation Workflow

**Implement ONE requirement at a time. For each:**

1. **Plan:** What code changes needed?
2. **Implement:** Write code
3. **Test:** Local test (mock data if needed)
4. **Verify:** Confirm behavior
5. **Commit:** Create commit with clear message
6. **Move to next:** Once verified, start next requirement

**Order:**

### Step 1: Scheduler + Local Testing
- [ ] Init Go module, basic main.go
- [ ] Add robfig/cron scheduler
- [ ] Implement cron trigger at peak hours (test with fixed time)
- [ ] Log when cron fires
- **Test:** Run locally, verify cron fires at specified times
- **Commit:** "Add cron scheduler for peak hours"

### Step 2: Profile Fetcher
- [ ] Implement HTTP client
- [ ] Fetch CPU profile from jago-service LB
- [ ] Fetch RAM/heap profile
- [ ] Store raw .prof files locally
- [ ] Convert to text via `go tool pprof -text`
- **Test:** Mock HTTP endpoint locally, verify profiles fetched + converted
- **Commit:** "Add profile fetcher (CPU + heap)"

### Step 3: SQLite Storage
- [ ] Setup SQLite database (check-in `profiler.db`)
- [ ] Create schema (profiles table)
- [ ] Implement insert (save profile metadata)
- [ ] Implement query (fetch yesterday's data by date)
- **Test:** Insert sample profile, query by date, verify retrieval
- **Commit:** "Add SQLite storage for profile metadata"

### Step 4: Claude Opus Integration
- [ ] Setup Anthropic SDK
- [ ] Implement prompt builder (profile text + yday data)
- [ ] Call Opus API
- [ ] Parse response (summary + anomalies)
- [ ] Implement 5% variance threshold detection
- **Test:** Mock profile, query yday from DB, send to Opus, parse response
- **Commit:** "Add Claude Opus analyzer with 5% anomaly threshold"

### Step 5: GitHub Integration
- [ ] Implement GitHub API client (or git CLI)
- [ ] Save profiles to `profiles/` directory
- [ ] Commit + push to GitHub repo
- [ ] Generate GitHub issue links (no auto-create)
- **Test:** Commit profiles locally, verify they appear in repo
- **Commit:** "Add GitHub profile storage + issue prompt links"

### Step 6: Slack Notifier
- [ ] Setup Slack bot token
- [ ] Implement Slack API client
- [ ] Format message (summary + anomalies + issue links)
- [ ] Send to Slack channel
- **Test:** Send test message, verify in Slack
- **Commit:** "Add Slack notification with summary + issue prompts"

### Step 7: Multi-Instance Routing
- [ ] Verify jago-service LB endpoint resolves correctly
- [ ] Confirm profiler hits one instance per run
- **Test:** Profile jago-service, check logs for single instance
- **Commit:** "Verify LB routing for profile fetches"

### Step 8: Deployment to Railway
- [ ] Create Dockerfile
- [ ] Create railway.toml (cron job config)
- [ ] Set environment variables
- [ ] Deploy to Railway
- [ ] Monitor logs, verify cron runs
- **Test:** Wait for scheduled run, check logs + Slack notification
- **Commit:** "Deploy profiler service to Railway"

### Step 9: Polish + Error Handling
- [ ] Add retry logic (failed profiles)
- [ ] Add structured logging
- [ ] Add health check endpoint
- [ ] Add graceful shutdown
- **Test:** Kill service mid-run, verify cleanup
- **Commit:** "Add error handling + logging"

### Step 10: Documentation
- [ ] Update README with setup instructions
- [ ] Document env vars + secrets
- [ ] Add troubleshooting guide
- **Commit:** "Add deployment documentation"

---

## Deployment to Railway

```bash
# 1. Create Railway project
railway init

# 2. Connect GitHub repo
# (Railway detects go.mod, auto-builds)

# 3. Set environment variables
railway env add JAGO_SERVICE_URL https://jago-service.jagocoffee.dev
railway env add SLACK_BOT_TOKEN xoxb-...
railway env add SLACK_CHANNEL_ID C...
railway env add GITHUB_TOKEN ghp_...
railway env add ANTHROPIC_API_KEY sk-ant-...

# 4. Deploy
git push origin main
# Railway auto-deploys on push

# 5. Monitor logs
railway logs
```

---

## Multi-Instance Strategy

**jago-service has N instances (Railway replicas or pods):**
- Profiler hits **load balancer endpoint** (`https://jago-service.jagocoffee.dev`)
- LB routes to random instance
- One profile per run = representative sample
- No need to profile all instances separately (unless debugging specific replica)

---

## Slack Notification Format

```
📊 Profiler Report — 2026-04-15 07:00 UTC+7

Summary:
CPU usage stable, peak at 45%. Heap growth +2% vs yday (normal GC pattern). 
No anomalies detected. Request latency p99 held steady.

Anomalies: None

Interested in opening issues?
🔗 [Investigate CPU peak](https://github.com/jagocoffee/jago-service/issues/new?title=Investigate+CPU+peak&body=...)
🔗 [Monitor memory trend](https://github.com/jagocoffee/jago-service/issues/new?title=Monitor+memory+trend&body=...)

Raw profiles: https://github.com/jagocoffee/jago-service/tree/main/profiles/2026-04-15-0700
```

---

## Success Criteria

- ✅ Runs exactly at 07:00, 08:00, 12:00, 13:00 UTC+7
- ✅ Fetches CPU + RAM profiles from jago-service
- ✅ Stores profiles in GitHub (committed daily)
- ✅ Analyzes with Claude Opus (1-paragraph summary)
- ✅ Compares against yesterday's data
- ✅ Flags anomalies >5% variance
- ✅ Sends Slack notification within 2 min of profile completion
- ✅ Prompts user to create GitHub issues (no auto-create)
- ✅ Runs on Railway without manual intervention
- ✅ Handles profile fetch failures gracefully (retry, alert)

---

## Open Questions

- [ ] Slack channel ID + API token (deferred)
- [ ] Want to keep profiles forever, or prune (>30 days)?
- [ ] Should profiler also trace goroutines/locks?
