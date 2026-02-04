package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/google/uuid"
)

// JobManager handles job execution with per-user queues
type JobManager struct {
	mu       sync.Mutex
	queues   map[int64]chan *Job
	workers  map[int64]bool
	wg       sync.WaitGroup
	shutdown chan struct{}
}

var jobManager *JobManager

// NewJobManager creates a new job manager
func NewJobManager() *JobManager {
	return &JobManager{
		queues:   make(map[int64]chan *Job),
		workers:  make(map[int64]bool),
		shutdown: make(chan struct{}),
	}
}

// Start initializes the job manager
func (jm *JobManager) Start() {
	log.Println("Job manager started")
}

// Stop shuts down the job manager gracefully
func (jm *JobManager) Stop() {
	close(jm.shutdown)
	jm.wg.Wait()
	log.Println("Job manager stopped")
}

// CreateJob creates a new job and queues it for execution
func (jm *JobManager) CreateJob(userID int64, jobType string) (*Job, error) {
	jobID := uuid.New().String()

	job, err := CreateJob(jobID, userID, jobType)
	if err != nil {
		return nil, err
	}

	// Ensure user has a queue and worker
	jm.mu.Lock()
	if _, ok := jm.queues[userID]; !ok {
		jm.queues[userID] = make(chan *Job, 10)
	}
	if !jm.workers[userID] {
		jm.workers[userID] = true
		jm.wg.Add(1)
		go jm.workerLoop(userID)
	}
	jm.mu.Unlock()

	// Queue the job
	jm.queues[userID] <- job

	return job, nil
}

// workerLoop processes jobs for a specific user
func (jm *JobManager) workerLoop(userID int64) {
	defer jm.wg.Done()

	queue := jm.queues[userID]

	for {
		select {
		case <-jm.shutdown:
			return
		case job := <-queue:
			jm.executeJob(job)
		}
	}
}

// executeJob runs a job
func (jm *JobManager) executeJob(job *Job) {
	log.Printf("Starting job %s (type: %s) for user %d", job.ID, job.Type, job.UserID)

	// Update status to running
	UpdateJobStatus(job.ID, "running", nil)

	// Create job logger
	logger := NewJobLogger(job.ID)

	// Get user config
	cfg, err := GetUserConfig(job.UserID)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to get user config: %v", err)
		logger.Log(errMsg)
		UpdateJobStatus(job.ID, "failed", &errMsg)
		logger.Save()
		return
	}

	// Create screenshot directory
	screenshotDir := filepath.Join(screenshotsPath, fmt.Sprintf("%d", job.UserID), job.ID)
	if err := os.MkdirAll(screenshotDir, 0755); err != nil {
		errMsg := fmt.Sprintf("Failed to create screenshot dir: %v", err)
		logger.Log(errMsg)
		UpdateJobStatus(job.ID, "failed", &errMsg)
		logger.Save()
		return
	}

	// Create browser context
	ctx, cancel := createJobBrowserContext()
	defer cancel()

	// Set job timeout
	jobCtx, jobCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer jobCancel()

	// Create screenshot helper
	saveScreenshot := func(name string) {
		filename := fmt.Sprintf("%s.png", name)
		path := filepath.Join(screenshotDir, filename)
		if err := SaveScreenshotToPath(jobCtx, path); err != nil {
			logger.Log(fmt.Sprintf("Failed to save screenshot %s: %v", name, err))
		} else {
			CreateScreenshot(job.ID, job.UserID, filename)
			logger.Log(fmt.Sprintf("Screenshot saved: %s", name))
		}
	}

	var jobErr error

	switch job.Type {
	case "test-login":
		jobErr = jm.runTestLoginJob(jobCtx, cfg, logger, saveScreenshot)
	case "test-check":
		jobErr = jm.runTestCheckJob(jobCtx, cfg, logger, saveScreenshot)
	case "full":
		jobErr = jm.runFullJob(jobCtx, cfg, logger, saveScreenshot)
	}

	if jobErr != nil {
		errMsg := jobErr.Error()
		logger.Log(fmt.Sprintf("Job failed: %s", errMsg))
		saveScreenshot("error_final")
		UpdateJobStatus(job.ID, "failed", &errMsg)
	} else {
		logger.Log("Job completed successfully")
		UpdateJobStatus(job.ID, "completed", nil)
	}

	logger.Save()
	log.Printf("Job %s completed", job.ID)
}

// runTestLoginJob tests only the login functionality
func (jm *JobManager) runTestLoginJob(ctx context.Context, cfg *UserConfig, logger *JobLogger, saveScreenshot func(string)) error {
	logger.Log("Starting login test")

	if err := GasolinaLogin(ctx, cfg.GasolinaEmail, cfg.GasolinaPassword, cfg.AccountNumber, logger, saveScreenshot); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	saveScreenshot("login_success")
	logger.Log("Login test passed")
	return nil
}

// runTestCheckJob tests login and check functionality
func (jm *JobManager) runTestCheckJob(ctx context.Context, cfg *UserConfig, logger *JobLogger, saveScreenshot func(string)) error {
	logger.Log("Starting check test")

	if err := GasolinaLogin(ctx, cfg.GasolinaEmail, cfg.GasolinaPassword, cfg.AccountNumber, logger, saveScreenshot); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Convert UserConfig to legacy Config for CheckAndUpdateIfNeeded
	legacyCfg := &Config{
		Email:             cfg.GasolinaEmail,
		Password:          cfg.GasolinaPassword,
		AccountNumber:     cfg.AccountNumber,
		CheckURL:          cfg.CheckURL,
		CronSchedule:      cfg.CronSchedule,
		DryRun:            cfg.DryRun,
		MonthlyIncrements: cfg.MonthlyIncrements,
	}

	if err := CheckAndUpdateIfNeededWithLogger(ctx, legacyCfg, logger, saveScreenshot); err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	saveScreenshot("check_success")
	logger.Log("Check test passed")
	return nil
}

// runFullJob runs the complete automation job
func (jm *JobManager) runFullJob(ctx context.Context, cfg *UserConfig, logger *JobLogger, saveScreenshot func(string)) error {
	logger.Log("Starting full job")

	// Login with retry
	var loginErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			waitTime := time.Duration(i*2) * time.Second
			logger.Log(fmt.Sprintf("Retry %d/3 after %v...", i+1, waitTime))
			time.Sleep(waitTime)
		}

		loginErr = GasolinaLogin(ctx, cfg.GasolinaEmail, cfg.GasolinaPassword, cfg.AccountNumber, logger, saveScreenshot)
		if loginErr == nil {
			break
		}
		logger.Log(fmt.Sprintf("Login attempt %d/3 failed: %v", i+1, loginErr))
	}

	if loginErr != nil {
		return fmt.Errorf("login failed after retries: %w", loginErr)
	}

	// Convert UserConfig to legacy Config
	legacyCfg := &Config{
		Email:             cfg.GasolinaEmail,
		Password:          cfg.GasolinaPassword,
		AccountNumber:     cfg.AccountNumber,
		CheckURL:          cfg.CheckURL,
		CronSchedule:      cfg.CronSchedule,
		DryRun:            cfg.DryRun,
		MonthlyIncrements: cfg.MonthlyIncrements,
	}

	// Check and update with retry
	var checkErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			waitTime := time.Duration(i*2) * time.Second
			logger.Log(fmt.Sprintf("Retry %d/3 after %v...", i+1, waitTime))
			time.Sleep(waitTime)
		}

		checkErr = CheckAndUpdateIfNeededWithLogger(ctx, legacyCfg, logger, saveScreenshot)
		if checkErr == nil {
			break
		}
		logger.Log(fmt.Sprintf("Check attempt %d/3 failed: %v", i+1, checkErr))
	}

	if checkErr != nil {
		return fmt.Errorf("check and update failed after retries: %w", checkErr)
	}

	logger.Log("Full job completed successfully")
	return nil
}

// createJobBrowserContext creates a browser context for job execution
func createJobBrowserContext() (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))

	return ctx, cancel
}

// JobLogger collects logs for a job
type JobLogger struct {
	jobID string
	logs  []string
	mu    sync.Mutex
}

// NewJobLogger creates a new job logger
func NewJobLogger(jobID string) *JobLogger {
	return &JobLogger{
		jobID: jobID,
		logs:  make([]string, 0),
	}
}

// Log adds a log entry
func (jl *JobLogger) Log(message string) {
	jl.mu.Lock()
	defer jl.mu.Unlock()

	entry := fmt.Sprintf("%s %s", time.Now().Format(time.RFC3339), message)
	jl.logs = append(jl.logs, entry)
	log.Printf("[Job %s] %s", jl.jobID, message)
}

// Save persists logs to database
func (jl *JobLogger) Save() {
	jl.mu.Lock()
	defer jl.mu.Unlock()
	AppendJobLogs(jl.jobID, jl.logs)
}
