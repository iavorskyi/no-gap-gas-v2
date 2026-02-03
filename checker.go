package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// CheckAndUpdateIfNeeded navigates to the target page, checks values, and updates if needed
//
// DRY-RUN mode is controlled by GASOLINA_DRY_RUN env var (default: true/enabled)
// Set GASOLINA_DRY_RUN=false to enable real submissions.
//
// It will:
//   - Read current value from #last_value field
//   - Calculate new value (current + previous month's increment)
//   - Find the input field and "Ввести" button
//   - In dry-run mode: log what it would do and save a screenshot
//   - In live mode: actually submit the form
func CheckAndUpdateIfNeeded(ctx context.Context, config *Config) error {
	now := time.Now()
	currentDay := now.Day()
	currentMonth := int(now.Month())

	// Check if we're within the allowed submission window (1st-5th of month)
	if currentDay < 1 || currentDay > 5 {
		log.Printf("Today is day %d of the month - submission only allowed on days 1-5", currentDay)
		return fmt.Errorf("outside submission window (days 1-5)")
	}

	log.Printf("Day %d is within submission window (1-5) - proceeding", currentDay)

	// Get the increment for previous month (we submit consumption from last month)
	increment, prevMonth, err := config.GetIncrementForPreviousMonth(currentMonth)
	if err != nil {
		return fmt.Errorf("failed to get increment for previous month %d: %w", prevMonth, err)
	}

	log.Printf("Using increment from previous month %d: %d", prevMonth, increment)

	// First, navigate to main page to read current value from #last_value field
	log.Println("Navigating to main page to read current value from #last_value...")
	var currentValueStr string

	err = chromedp.Run(ctx,
		chromedp.Navigate("https://gasolina-online.com/"),
		chromedp.Sleep(2*time.Second),
		chromedp.WaitReady("body"),
		chromedp.WaitVisible(`#last_value`, chromedp.ByID),
		chromedp.Value(`#last_value`, &currentValueStr, chromedp.ByID),
	)

	if err != nil {
		return fmt.Errorf("failed to read #last_value from main page: %w", err)
	}

	if currentValueStr == "" {
		return fmt.Errorf("#last_value field is empty on main page")
	}

	log.Printf("Current value from #last_value field: %s", currentValueStr)

	// Parse current value
	var currentValue int
	_, err = fmt.Sscanf(currentValueStr, "%d", &currentValue)
	if err != nil {
		return fmt.Errorf("failed to parse current value '%s': %w", currentValueStr, err)
	}

	// Calculate new value
	newValue := currentValue + increment
	log.Printf("=== CALCULATED VALUE: %d + %d = %d ===", currentValue, increment, newValue)

	// Now navigate to indicator page to check for existing records
	log.Printf("Navigating to: %s", config.CheckURL)
	var pageContent string

	err = chromedp.Run(ctx,
		chromedp.Navigate(config.CheckURL),
		chromedp.Sleep(2*time.Second),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(`document.body.innerText`, &pageContent),
	)

	if err != nil {
		return fmt.Errorf("failed to navigate to indicator page: %w", err)
	}

	log.Printf("Indicator page content length: %d characters", len(pageContent))

	// Check if a record for the current month/year already exists
	if recordExists := checkForCurrentMonthRecord(pageContent, now); recordExists {
		log.Printf("Record for current month (%s %d) already exists - no update needed",
			getUkrainianMonthName(now.Month()), now.Year())
		return nil
	}

	log.Printf("No record found for current month (%s %d)",
		getUkrainianMonthName(now.Month()), now.Year())
	log.Printf("Proceeding to submit new value: %d", newValue)

	// Navigate back to main page where the "Ввести" button is located
	log.Println("Navigating back to main page to find 'Ввести' button...")
	err = chromedp.Run(ctx,
		chromedp.Navigate("https://gasolina-online.com/"),
		chromedp.Sleep(2*time.Second),
		chromedp.WaitReady("body"),
	)
	if err != nil {
		return fmt.Errorf("failed to navigate back to main page: %w", err)
	}

	// Find the modal trigger button (the "Ввести" button that opens the modal)
	// This button has data-toggle="modal" attribute
	var modalButtonFound bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('button[data-toggle="modal"][data-target="#counterModal"]') !== null`, &modalButtonFound),
	)

	if err != nil || !modalButtonFound {
		log.Println("WARNING: Could not find modal trigger button with data-toggle='modal'")
		_ = SaveScreenshot(ctx, fmt.Sprintf("no_modal_button_%d.png", time.Now().Unix()))
		return fmt.Errorf("modal trigger button not found on indicator page")
	}

	log.Println("Found modal trigger button (data-toggle='modal')")

	// Get button data attributes for logging
	var buttonSerial, buttonValue string
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('button[data-toggle="modal"][data-target="#counterModal"]').getAttribute('data-serial')`, &buttonSerial),
		chromedp.Evaluate(`document.querySelector('button[data-toggle="modal"][data-target="#counterModal"]').getAttribute('data-value')`, &buttonValue),
	)
	log.Printf("Modal button data: serial=%s, current_value=%s", buttonSerial, buttonValue)

	// Click the modal trigger button to open the modal
	log.Println("Clicking modal trigger button to open form...")
	err = chromedp.Run(ctx,
		chromedp.Click(`button[data-toggle="modal"][data-target="#counterModal"]`, chromedp.ByQuery),
		chromedp.Sleep(1*time.Second),
	)
	if err != nil {
		_ = SaveScreenshot(ctx, fmt.Sprintf("error_open_modal_%d.png", time.Now().Unix()))
		return fmt.Errorf("failed to click modal trigger button: %w", err)
	}

	// Wait for the modal to be visible
	log.Println("Waiting for modal to appear...")
	err = chromedp.Run(ctx,
		chromedp.WaitVisible(`#counterModal`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
	)
	if err != nil {
		_ = SaveScreenshot(ctx, fmt.Sprintf("error_modal_not_visible_%d.png", time.Now().Unix()))
		return fmt.Errorf("modal did not appear: %w", err)
	}

	log.Println("Modal is now visible")

	// Find the input field in the modal
	var inputFound bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#value') !== null`, &inputFound),
	)

	if err != nil || !inputFound {
		log.Println("WARNING: Could not find #value input field in modal")
		_ = SaveScreenshot(ctx, fmt.Sprintf("no_input_in_modal_%d.png", time.Now().Unix()))
		return fmt.Errorf("input field #value not found in modal")
	}

	log.Println("Found input field #value in modal")

	// Fill the input field with the new value
	log.Printf("Filling input field with new value: %d", newValue)
	err = chromedp.Run(ctx,
		chromedp.Clear(`#value`, chromedp.ByID),
		chromedp.SendKeys(`#value`, fmt.Sprintf("%d", newValue), chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
	)
	if err != nil {
		_ = SaveScreenshot(ctx, fmt.Sprintf("error_fill_input_%d.png", time.Now().Unix()))
		return fmt.Errorf("failed to fill input field: %w", err)
	}

	// Verify the value was entered
	var enteredValue string
	_ = chromedp.Run(ctx,
		chromedp.Value(`#value`, &enteredValue, chromedp.ByID),
	)
	log.Printf("Value entered in input field: %s", enteredValue)

	// DRY-RUN MODE - controlled by GASOLINA_DRY_RUN env var (default: true)
	// Set GASOLINA_DRY_RUN=false to enable real submissions

	if config.DryRun {
		log.Println("===========================================")
		log.Println("DRY-RUN MODE (set GASOLINA_DRY_RUN=false to submit)")
		log.Println("===========================================")
		log.Printf("Form data ready for submission:")
		log.Printf("  - Counter serial: %s", buttonSerial)
		log.Printf("  - Previous value: %s", buttonValue)
		log.Printf("  - New value: %d", newValue)
		log.Printf("  - Input field: #value")
		log.Printf("  - Value entered: %s", enteredValue)
		log.Println("===========================================")
		log.Println("SKIPPING submit button click (dry-run mode)")
		log.Println("===========================================")

		// Save screenshot of the filled form
		_ = SaveScreenshot(ctx, fmt.Sprintf("dry_run_form_filled_%d.png", time.Now().Unix()))
		return nil
	}

	// Find and click the submit button inside the modal
	log.Println("Finding submit button in modal...")
	var submitButtonFound bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(function() {
				const modal = document.querySelector('#counterModal');
				if (!modal) return false;
				const submitBtn = modal.querySelector('button[type="submit"]');
				return submitBtn !== null;
			})()
		`, &submitButtonFound),
	)

	if err != nil || !submitButtonFound {
		log.Println("WARNING: Could not find submit button in modal")
		_ = SaveScreenshot(ctx, fmt.Sprintf("no_submit_button_%d.png", time.Now().Unix()))
		return fmt.Errorf("submit button not found in modal")
	}

	log.Println("Found submit button, clicking...")
	err = chromedp.Run(ctx,
		chromedp.Click(`#counterModal button[type="submit"]`, chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		_ = SaveScreenshot(ctx, fmt.Sprintf("error_submit_%d.png", time.Now().Unix()))
		return fmt.Errorf("failed to click submit button: %w", err)
	}

	log.Println("Clicked submit button")

	// Verify submission success
	var successMessage string
	_ = chromedp.Run(ctx,
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(`document.body.innerText`, &successMessage),
	)

	if strings.Contains(strings.ToLower(successMessage), "успішно") ||
		strings.Contains(strings.ToLower(successMessage), "success") {
		log.Println("SUCCESS: Form submitted successfully!")
		_ = SaveScreenshot(ctx, fmt.Sprintf("success_%d.png", time.Now().Unix()))
	} else {
		log.Println("WARNING: Could not confirm success message")
		_ = SaveScreenshot(ctx, fmt.Sprintf("submit_complete_%d.png", time.Now().Unix()))
	}

	return nil
}

// checkForCurrentMonthRecord checks if a record for the current month/year exists on the page
func checkForCurrentMonthRecord(pageContent string, now time.Time) bool {
	// Get current month and year
	currentMonth := now.Month()
	currentYear := now.Year()

	// Ukrainian month name (e.g., "Січень 2026")
	ukrainianMonth := getUkrainianMonthName(currentMonth)
	pattern1 := fmt.Sprintf("%s %d", ukrainianMonth, currentYear)

	// Numeric formats
	pattern2 := fmt.Sprintf("%02d.%d", currentMonth, currentYear)       // 01.2026
	pattern3 := fmt.Sprintf("%d-%02d", currentYear, currentMonth)       // 2026-01
	pattern4 := fmt.Sprintf("%02d/%d", currentMonth, currentYear)       // 01/2026
	pattern5 := fmt.Sprintf("%s %04d", ukrainianMonth, currentYear%100) // Січень 26

	// Check all patterns
	patterns := []string{pattern1, pattern2, pattern3, pattern4, pattern5}
	for _, pattern := range patterns {
		if strings.Contains(pageContent, pattern) {
			log.Printf("Found matching pattern: %s", pattern)
			return true
		}
	}

	return false
}

// getUkrainianMonthName returns the Ukrainian name for a given month
func getUkrainianMonthName(month time.Month) string {
	monthNames := map[time.Month]string{
		time.January:   "Січень",
		time.February:  "Лютий",
		time.March:     "Березень",
		time.April:     "Квітень",
		time.May:       "Травень",
		time.June:      "Червень",
		time.July:      "Липень",
		time.August:    "Серпень",
		time.September: "Вересень",
		time.October:   "Жовтень",
		time.November:  "Листопад",
		time.December:  "Грудень",
	}

	return monthNames[month]
}
