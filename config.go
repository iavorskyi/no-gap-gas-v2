package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds the application configuration (legacy, for CLI mode)
type Config struct {
	Email             string
	Password          string
	AccountNumber     string
	CheckURL          string
	CronSchedule      string
	DryRun            bool
	MonthlyIncrements map[int]int // month number -> increment value
}

// AppConfig holds the HTTP server configuration
type AppConfig struct {
	// HTTP Server
	HTTPPort string

	// JWT
	JWTSecret        string
	JWTAccessExpiry  time.Duration
	JWTRefreshExpiry time.Duration

	// Database
	DBPath string

	// Screenshots
	ScreenshotsPath string

	// CORS
	CORSAllowedOrigins []string

	// Legacy config (for CLI mode)
	LegacyConfig *Config
}

// LoadAppConfig loads the application configuration from environment variables
func LoadAppConfig() (*AppConfig, error) {
	// Load .env file if it exists (ignore error if it doesn't)
	_ = godotenv.Load()

	cfg := &AppConfig{
		HTTPPort:        getEnvOrDefault("HTTP_PORT", "8080"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		DBPath:          getEnvOrDefault("DB_PATH", "./data/gasolina.db"),
		ScreenshotsPath: getEnvOrDefault("SCREENSHOTS_PATH", "./data/screenshots"),
	}

	// Parse JWT expiry durations
	accessExpiry := getEnvOrDefault("JWT_ACCESS_EXPIRY", "15m")
	if d, err := time.ParseDuration(accessExpiry); err == nil {
		cfg.JWTAccessExpiry = d
	} else {
		cfg.JWTAccessExpiry = 15 * time.Minute
	}

	refreshExpiry := getEnvOrDefault("JWT_REFRESH_EXPIRY", "168h") // 7 days
	if d, err := time.ParseDuration(refreshExpiry); err == nil {
		cfg.JWTRefreshExpiry = d
	} else {
		cfg.JWTRefreshExpiry = 7 * 24 * time.Hour
	}

	// Parse CORS origins
	corsOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if corsOrigins != "" {
		cfg.CORSAllowedOrigins = strings.Split(corsOrigins, ",")
		for i := range cfg.CORSAllowedOrigins {
			cfg.CORSAllowedOrigins[i] = strings.TrimSpace(cfg.CORSAllowedOrigins[i])
		}
	} else {
		cfg.CORSAllowedOrigins = []string{"*"}
	}

	return cfg, nil
}

// LoadConfig loads configuration from environment variables (legacy, for CLI mode)
func LoadConfig() (*Config, error) {
	// Load .env file if it exists (ignore error if it doesn't)
	_ = godotenv.Load()

	config := &Config{
		Email:         os.Getenv("GASOLINA_EMAIL"),
		Password:      os.Getenv("GASOLINA_PASSWORD"),
		AccountNumber: os.Getenv("GASOLINA_ACCOUNT_NUMBER"),
		CheckURL:      os.Getenv("GASOLINA_CHECK_URL"),
		CronSchedule:  os.Getenv("CRON_SCHEDULE"),
		DryRun:        os.Getenv("GASOLINA_DRY_RUN") != "false",
	}

	// Set default cron schedule if not provided
	if config.CronSchedule == "" {
		config.CronSchedule = "0 0 1 * *" // 1st day of month at midnight
	}

	// Parse monthly increments JSON
	monthlyIncrementsJSON := os.Getenv("GASOLINA_MONTHLY_INCREMENTS")
	if monthlyIncrementsJSON == "" {
		return nil, fmt.Errorf("GASOLINA_MONTHLY_INCREMENTS is required")
	}

	if err := json.Unmarshal([]byte(monthlyIncrementsJSON), &config.MonthlyIncrements); err != nil {
		return nil, fmt.Errorf("failed to parse GASOLINA_MONTHLY_INCREMENTS: %w", err)
	}

	// Validate required fields
	if config.Email == "" {
		return nil, fmt.Errorf("GASOLINA_EMAIL is required")
	}
	if config.Password == "" {
		return nil, fmt.Errorf("GASOLINA_PASSWORD is required")
	}
	if config.AccountNumber == "" {
		return nil, fmt.Errorf("GASOLINA_ACCOUNT_NUMBER is required")
	}
	if config.CheckURL == "" {
		return nil, fmt.Errorf("GASOLINA_CHECK_URL is required")
	}
	if len(config.MonthlyIncrements) == 0 {
		return nil, fmt.Errorf("GASOLINA_MONTHLY_INCREMENTS must contain at least one month")
	}

	return config, nil
}

// GetIncrementForMonth returns the increment value for a given month (1-12)
func (c *Config) GetIncrementForMonth(month int) (int, error) {
	increment, ok := c.MonthlyIncrements[month]
	if !ok {
		return 0, fmt.Errorf("no increment configured for month %d", month)
	}
	return increment, nil
}

// GetIncrementForPreviousMonth returns the increment value for the previous month
// If current month is January (1), returns December (12) increment
func (c *Config) GetIncrementForPreviousMonth(currentMonth int) (int, int, error) {
	prevMonth := currentMonth - 1
	if prevMonth < 1 {
		prevMonth = 12
	}
	increment, err := c.GetIncrementForMonth(prevMonth)
	return increment, prevMonth, err
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
