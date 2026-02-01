package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/robfig/cron/v3"
)

var (
	testLogin = flag.Bool("test-login", false, "Test login functionality only")
	testCheck = flag.Bool("test-check", false, "Test checker functionality only")
	runNow    = flag.Bool("now", false, "Run the job immediately instead of waiting for schedule")
)

func main() {
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Gasolina Online Automation Service")

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
