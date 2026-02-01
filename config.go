package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds the application configuration
type Config struct {
	Email             string
	Password          string
	AccountNumber     string
	CheckURL          string
	CronSchedule      string
	DryRun            bool
	MonthlyIncrements map[int]int // month number -> increment value
}

// LoadConfig loads configuration from environment variables
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
