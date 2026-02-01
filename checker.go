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

	// Find the input field for entering the new value
	// Common patterns: input[name="value"], input[type="text"], input[type="number"]
	var inputFound bool
	var inputSelector string

	possibleSelectors := []string{
		`input[name="value"]`,
		`input[name="indicator_value"]`,
		`input[name="new_value"]`,
		`input[type="number"]`,
		`input[type="text"]:not([disabled])`,
	}

	// Try to find the input field
	for _, selector := range possibleSelectors {
		err = chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`document.querySelector('%s') !== null`, selector), &inputFound),
		)
		if err == nil && inputFound {
			inputSelector = selector
			log.Printf("Found input field with selector: %s", selector)
			break
		}
	}

	if !inputFound || inputSelector == "" {
		log.Println("WARNING: Could not find input field for new value")
		log.Println("Will attempt to find 'Ввести' button anyway")
	}

	// Find the "Ввести" button
	var buttonFound bool
	var buttonSelector string

	possibleButtonSelectors := []string{
		`button:contains("Ввести")`,
		`input[type="submit"][value*="Ввести"]`,
		`button[type="submit"]:contains("Ввести")`,
		`a:contains("Ввести")`,
	}

	for _, selector := range possibleButtonSelectors {
		err = chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`
				(function() {
					const elements = document.querySelectorAll('button, input[type="submit"], a');
					for (let el of elements) {
						if (el.textContent.includes('Ввести') || (el.value && el.value.includes('Ввести'))) {
							return true;
						}
					}
					return false;
				})()
			`), &buttonFound),
		)
		if err == nil && buttonFound {
			buttonSelector = selector
			log.Printf("Found 'Ввести' button with selector: %s", selector)
			break
		}
	}

	if !buttonFound {
		log.Println("WARNING: Could not find 'Ввести' button")
		_ = SaveScreenshot(ctx, fmt.Sprintf("no_button_found_%d.png", time.Now().Unix()))
		return fmt.Errorf("'Ввести' button not found on indicator page")
	}

	// Check if button is enabled
	var buttonEnabled bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(function() {
				const buttons = document.querySelectorAll('button, input[type="submit"], a');
				for (let btn of buttons) {
					if (btn.textContent.includes('Ввести') || (btn.value && btn.value.includes('Ввести'))) {
						return !btn.disabled;
					}
				}
				return false;
			})()
		`, &buttonEnabled),
	)

	if err == nil {
		if buttonEnabled {
			log.Println("'Ввести' button is ENABLED")
		} else {
			log.Println("WARNING: 'Ввести' button is DISABLED - may need to be within days 1-5")
		}
	}

	// DRY-RUN MODE - controlled by GASOLINA_DRY_RUN env var (default: true)
	// Set GASOLINA_DRY_RUN=false to enable real submissions

	if config.DryRun {
		log.Println("===========================================")
		log.Println("DRY-RUN MODE (set GASOLINA_DRY_RUN=false to submit)")
		log.Println("===========================================")
		log.Printf("Would perform the following actions:")
		if inputFound {
			log.Printf("  1. Clear input field: %s", inputSelector)
			log.Printf("  2. Enter new value: %d", newValue)
		}
		log.Printf("  3. Click button: %s", buttonSelector)
		log.Printf("  4. Wait for submission to complete")
		log.Printf("  5. Verify success message")
		log.Println("===========================================")
		log.Println("SKIPPING actual submission (dry-run mode)")
		log.Println("===========================================")

		// Save screenshot of the state
		_ = SaveScreenshot(ctx, fmt.Sprintf("dry_run_ready_%d.png", time.Now().Unix()))
		return nil
	}

	// The code below would execute if DRY_RUN were set to false
	// LEFT HERE FOR FUTURE REFERENCE - NOT EXECUTED
	log.Println("Filling form and submitting...")

	if inputFound && inputSelector != "" {
		err = chromedp.Run(ctx,
			chromedp.Clear(inputSelector, chromedp.ByQuery),
			chromedp.SendKeys(inputSelector, fmt.Sprintf("%d", newValue), chromedp.ByQuery),
			chromedp.Sleep(1*time.Second),
		)
		if err != nil {
			return fmt.Errorf("failed to enter new value: %w", err)
		}
		log.Printf("Entered value: %d", newValue)
	}

	// Click the button
	err = chromedp.Run(ctx,
		chromedp.Click(buttonSelector, chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		_ = SaveScreenshot(ctx, fmt.Sprintf("error_submit_%d.png", time.Now().Unix()))
		return fmt.Errorf("failed to click 'Ввести' button: %w", err)
	}

	log.Println("Clicked 'Ввести' button")

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
