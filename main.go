package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/robfig/cron/v3"
)

var (
	testLogin  = flag.Bool("test-login", false, "Test login functionality only")
	testCheck  = flag.Bool("test-check", false, "Test checker functionality only")
	runNow     = flag.Bool("now", false, "Run the job immediately instead of waiting for schedule")
	serverMode = flag.Bool("server", false, "Run in HTTP server mode")
)

func main() {
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Gasolina Online Automation Service")

	// Check if running in server mode
	if *serverMode {
		runServer()
		return
	}

	// Legacy CLI mode
	runCLIMode()
}

// runServer starts the HTTP server
func runServer() {
	// Load app configuration
	appCfg, err := LoadAppConfig()
	if err != nil {
		log.Fatalf("Failed to load app configuration: %v", err)
	}

	// Validate JWT secret
	if appCfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET environment variable is required for server mode")
	}
	if len(appCfg.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must be at least 32 characters")
	}

	// Validate database URL
	if appCfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required for server mode")
	}

	// Ensure screenshots directory exists
	if err := os.MkdirAll(appCfg.ScreenshotsPath, 0755); err != nil {
		log.Fatalf("Failed to create screenshots directory: %v", err)
	}

	// Initialize database
	if err := InitDB(appCfg.DatabaseURL); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer CloseDB()

	// Configure auth
	SetJWTConfig(appCfg.JWTSecret, appCfg.JWTAccessExpiry, appCfg.JWTRefreshExpiry)
	SetEncryptionKey(appCfg.JWTSecret)
	SetScreenshotsPath(appCfg.ScreenshotsPath)

	// Initialize job manager
	jobManager = NewJobManager()
	jobManager.Start()
	defer jobManager.Stop()

	// Create router
	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/auth/register", handleRegister)
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/auth/refresh", handleRefresh)
	mux.HandleFunc("/api/auth/logout", handleLogout)

	// Protected routes - wrapped with auth middleware
	mux.Handle("/api/me", AuthMiddleware(http.HandlerFunc(handleGetMe)))
	mux.Handle("/api/me/password", AuthMiddleware(http.HandlerFunc(handleChangePassword)))
	mux.Handle("/api/config", AuthMiddleware(http.HandlerFunc(handleConfig)))
	mux.Handle("/api/jobs", AuthMiddleware(http.HandlerFunc(handleJobs)))
	mux.Handle("/api/jobs/", AuthMiddleware(http.HandlerFunc(handleJobsWithID)))
	mux.Handle("/api/screenshots/", AuthMiddleware(http.HandlerFunc(handleScreenshotsRoute)))
	mux.Handle("/api/status", AuthMiddleware(http.HandlerFunc(handleStatus)))

	// Apply CORS middleware
	handler := CORSMiddleware(appCfg.CORSAllowedOrigins)(mux)

	// Create server
	server := &http.Server{
		Addr:         ":" + appCfg.HTTPPort,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("HTTP server listening on port %s", appCfg.HTTPPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Shutdown complete")
}

// handleConfig routes GET/PUT for /api/config
func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetConfig(w, r)
	case http.MethodPut:
		handleUpdateConfig(w, r)
	default:
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleJobs routes GET/POST for /api/jobs
func handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleListJobs(w, r)
	case http.MethodPost:
		handleCreateJob(w, r)
	default:
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleJobsWithID handles /api/jobs/{id}
func handleJobsWithID(w http.ResponseWriter, r *http.Request) {
	// Extract job ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	if path == "" {
		jsonError(w, "Job ID required", http.StatusBadRequest)
		return
	}
	handleGetJob(w, r, path)
}

// handleScreenshotsRoute handles /api/screenshots/{job_id} and /api/screenshots/{job_id}/{filename}
func handleScreenshotsRoute(w http.ResponseWriter, r *http.Request) {
	// Extract path parts
	path := strings.TrimPrefix(r.URL.Path, "/api/screenshots/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) == 0 || parts[0] == "" {
		jsonError(w, "Job ID required", http.StatusBadRequest)
		return
	}

	jobID := parts[0]

	if len(parts) == 1 {
		// List screenshots for job
		handleListScreenshots(w, r, jobID)
	} else {
		// Get specific screenshot
		handleGetScreenshot(w, r, jobID, parts[1])
	}
}

// runCLIMode runs the legacy CLI mode
func runCLIMode() {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Configuration loaded successfully")
	log.Printf("Cron schedule: %s", config.CronSchedule)
	log.Printf("Account number: %s", config.AccountNumber)
	log.Printf("Target URL: %s", config.CheckURL)
	log.Printf("Dry-run mode: %v", config.DryRun)

	// Test mode handlers
	if *testLogin {
		log.Println("Running in TEST LOGIN mode")
		runTestLogin(config)
		return
	}

	if *testCheck {
		log.Println("Running in TEST CHECK mode")
		runTestCheck(config)
		return
	}

	if *runNow {
		log.Println("Running job immediately")
		runJob(config)
		return
	}

	// Create cron scheduler
	c := cron.New(cron.WithLogger(cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))))

	// Register the job
	_, err = c.AddFunc(config.CronSchedule, func() {
		log.Println("=== Scheduled job triggered ===")
		runJob(config)
	})

	if err != nil {
		log.Fatalf("Failed to schedule job: %v", err)
	}

	// Start the scheduler
	c.Start()
	log.Println("Scheduler started. Waiting for scheduled jobs...")

	// Wait for termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")
	ctx := c.Stop()
	<-ctx.Done()
	log.Println("Shutdown complete")
}

// runJob executes the main automation job
func runJob(config *Config) {
	ctx, cancel := createBrowserContext()
	defer cancel()

	// Set a timeout for the entire job
	jobCtx, jobCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer jobCancel()

	// Login
	if err := retryWithBackoff(jobCtx, 3, func() error {
		return Login(jobCtx, config.Email, config.Password, config.AccountNumber)
	}); err != nil {
		log.Printf("ERROR: Login failed after retries: %v", err)
		_ = SaveScreenshot(jobCtx, "error_login.png")
		return
	}

	// Check and update if needed
	if err := retryWithBackoff(jobCtx, 3, func() error {
		return CheckAndUpdateIfNeeded(jobCtx, config)
	}); err != nil {
		log.Printf("ERROR: Check and update failed after retries: %v", err)
		_ = SaveScreenshot(jobCtx, "error_check.png")
		return
	}

	log.Println("=== Job completed successfully ===")
}

// runTestLogin tests only the login functionality
func runTestLogin(config *Config) {
	ctx, cancel := createBrowserContext()
	defer cancel()

	if err := Login(ctx, config.Email, config.Password, config.AccountNumber); err != nil {
		log.Printf("Login test FAILED: %v", err)
		_ = SaveScreenshot(ctx, "test_login_error.png")
		os.Exit(1)
	}

	// Save screenshot on success
	_ = SaveScreenshot(ctx, "test_login_success.png")
	log.Println("Login test PASSED")
}

// runTestCheck tests only the checker functionality (assumes already logged in or public page)
func runTestCheck(config *Config) {
	ctx, cancel := createBrowserContext()
	defer cancel()

	// Try to login first
	if err := Login(ctx, config.Email, config.Password, config.AccountNumber); err != nil {
		log.Printf("Warning: Login failed: %v", err)
	}

	if err := CheckAndUpdateIfNeeded(ctx, config); err != nil {
		log.Printf("Check test FAILED: %v", err)
		_ = SaveScreenshot(ctx, "test_check_error.png")
		os.Exit(1)
	}

	_ = SaveScreenshot(ctx, "test_check_success.png")
	log.Println("Check test PASSED")
}

// createBrowserContext creates a new browser context for automation
func createBrowserContext() (context.Context, context.CancelFunc) {
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

// retryWithBackoff retries a function with exponential backoff
func retryWithBackoff(ctx context.Context, maxRetries int, fn func() error) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			waitTime := time.Duration(i*2) * time.Second
			log.Printf("Retry %d/%d after %v...", i+1, maxRetries, waitTime)
			time.Sleep(waitTime)
		}

		err = fn()
		if err == nil {
			return nil
		}

		log.Printf("Attempt %d/%d failed: %v", i+1, maxRetries, err)
	}

	return err
}

func init() {
	// Print usage help
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Gasolina Online Automation Service\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s [flags]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nModes:\n")
		fmt.Fprintf(os.Stderr, "  CLI mode (default): Requires GASOLINA_* env vars, runs cron scheduler\n")
		fmt.Fprintf(os.Stderr, "  Server mode (-server): Runs HTTP API, requires JWT_SECRET env var\n")
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables (Server mode):\n")
		fmt.Fprintf(os.Stderr, "  JWT_SECRET            Required. Secret for JWT signing (min 32 chars)\n")
		fmt.Fprintf(os.Stderr, "  DATABASE_URL          Required. PostgreSQL connection URL\n")
		fmt.Fprintf(os.Stderr, "  HTTP_PORT             HTTP port (default: 8080)\n")
		fmt.Fprintf(os.Stderr, "  SCREENSHOTS_PATH      Screenshots directory (default: ./data/screenshots)\n")
		fmt.Fprintf(os.Stderr, "  CORS_ALLOWED_ORIGINS  Comma-separated CORS origins (default: *)\n")
	}
}
