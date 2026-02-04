package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

// Logger interface for job logging
type Logger interface {
	Log(message string)
}

// defaultLogger implements Logger using standard log
type defaultLogger struct{}

func (d *defaultLogger) Log(message string) {
	log.Println(message)
}

// GasolinaLogin performs authentication on gasolina-online.com
// This is the refactored version that accepts logger and screenshot callback
func GasolinaLogin(ctx context.Context, email, password, accountNumber string, logger Logger, saveScreenshot func(string)) error {
	if logger == nil {
		logger = &defaultLogger{}
	}
	if saveScreenshot == nil {
		saveScreenshot = func(name string) {}
	}

	logger.Log(fmt.Sprintf("Attempting to login as %s...", email))

	var loginURL = "https://gasolina-online.com/"

	// Navigate and wait for page load
	err := chromedp.Run(ctx,
		chromedp.Navigate(loginURL),
		chromedp.Sleep(3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}

	// Save screenshot to see the page state
	saveScreenshot("debug_before_login")
	logger.Log("Screenshot saved: debug_before_login")

	// Check what elements are on the page
	var pageHTML string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.documentElement.outerHTML`, &pageHTML),
	)
	if err != nil {
		logger.Log(fmt.Sprintf("Warning: couldn't get page HTML: %v", err))
	} else {
		logger.Log(fmt.Sprintf("Page HTML length: %d characters", len(pageHTML)))
	}

	// Try to find input fields with various selectors
	emailSelectors := []string{
		`input[type="email"]`,
		`input[name="email"]`,
		`input[id="email"]`,
		`input[placeholder*="email" i]`,
		`input[placeholder*="пошта" i]`,
		`input[placeholder*="Email" i]`,
	}

	passwordSelectors := []string{
		`input[type="password"]`,
		`input[name="password"]`,
		`input[id="password"]`,
		`input[placeholder*="пароль" i]`,
		`input[placeholder*="Password" i]`,
	}

	// Try each email selector
	emailFound := false
	for _, selector := range emailSelectors {
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.SendKeys(selector, email, chromedp.ByQuery),
		)
		if err == nil {
			logger.Log(fmt.Sprintf("Email field found with selector: %s", selector))
			emailFound = true
			break
		}
	}

	if !emailFound {
		return fmt.Errorf("email field not found - check debug_before_login screenshot")
	}

	// Try each password selector
	passwordFound := false
	for _, selector := range passwordSelectors {
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.SendKeys(selector, password, chromedp.ByQuery),
		)
		if err == nil {
			logger.Log(fmt.Sprintf("Password field found with selector: %s", selector))
			passwordFound = true
			break
		}
	}

	if !passwordFound {
		return fmt.Errorf("password field not found - check debug_before_login screenshot")
	}

	// Try to find and click the login button
	buttonSelectors := []string{
		`button[type="submit"]`,
		`input[type="submit"]`,
		`button:contains("Увійти")`,
		`button:contains("Вхід")`,
		`button:contains("Login")`,
		`button:contains("Sign in")`,
		`a:contains("Увійти")`,
	}

	buttonFound := false
	for _, selector := range buttonSelectors {
		err = chromedp.Run(ctx,
			chromedp.Click(selector, chromedp.ByQuery),
		)
		if err == nil {
			logger.Log(fmt.Sprintf("Login button found with selector: %s", selector))
			buttonFound = true
			break
		}
	}

	if !buttonFound {
		logger.Log("Warning: login button not found, trying to submit form with Enter key")
		// Try pressing Enter in the password field
		err = chromedp.Run(ctx,
			chromedp.SendKeys(`input[type="password"]`, "\n", chromedp.ByQuery),
		)
		if err != nil {
			return fmt.Errorf("couldn't submit login form")
		}
	}

	// Wait for navigation after login
	chromedp.Run(ctx, chromedp.Sleep(3*time.Second))

	// Save screenshot after login attempt
	saveScreenshot("debug_after_login")
	logger.Log("Screenshot saved: debug_after_login")

	// Select account from dropdown
	if accountNumber != "" {
		logger.Log(fmt.Sprintf("Selecting account containing: %s", accountNumber))

		// First, click the hamburger menu to open navigation using JavaScript
		err = chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('.navbar-toggler').click()`, nil),
			chromedp.Sleep(1*time.Second),
		)
		if err != nil {
			logger.Log(fmt.Sprintf("Hamburger menu click failed: %v", err))
		}

		saveScreenshot("debug_menu_open")
		logger.Log("Screenshot saved: debug_menu_open")

		// Click the account dropdown toggle button using JavaScript
		err = chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('#dropdown01').click()`, nil),
			chromedp.Sleep(1*time.Second),
		)
		if err != nil {
			return fmt.Errorf("failed to click account dropdown: %w", err)
		}

		saveScreenshot("debug_dropdown_open")
		logger.Log("Screenshot saved: debug_dropdown_open")

		// Find and click the dropdown item containing the account number using JavaScript
		jsClick := fmt.Sprintf(`
			const links = document.querySelectorAll('.dropdown-menu a.dropdown-item');
			for (const link of links) {
				if (link.textContent.includes('%s')) {
					link.click();
					break;
				}
			}
		`, accountNumber)
		err = chromedp.Run(ctx,
			chromedp.Evaluate(jsClick, nil),
			chromedp.Sleep(2*time.Second),
		)
		if err != nil {
			return fmt.Errorf("failed to select account %s: %w", accountNumber, err)
		}

		saveScreenshot("debug_account_selected")
		logger.Log(fmt.Sprintf("Account %s selected successfully", accountNumber))
	}

	logger.Log("Login sequence completed")
	return nil
}

// Login is the legacy function for backwards compatibility with CLI mode
func Login(ctx context.Context, email, password, accountNumber string) error {
	// Use old-style screenshot saving for CLI mode
	saveScreenshot := func(name string) {
		SaveScreenshot(ctx, name+".png")
	}
	return GasolinaLogin(ctx, email, password, accountNumber, nil, saveScreenshot)
}

// SaveScreenshot saves a screenshot for debugging purposes (legacy, for CLI mode)
func SaveScreenshot(ctx context.Context, filename string) error {
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		return fmt.Errorf("failed to capture screenshot: %w", err)
	}

	if err := os.WriteFile(filename, buf, 0644); err != nil {
		return fmt.Errorf("failed to save screenshot: %w", err)
	}

	log.Printf("Screenshot saved: %s", filename)
	return nil
}

// SaveScreenshotToPath saves a screenshot to a specific path (for job mode)
func SaveScreenshotToPath(ctx context.Context, path string) error {
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		return fmt.Errorf("failed to capture screenshot: %w", err)
	}

	if err := os.WriteFile(path, buf, 0644); err != nil {
		return fmt.Errorf("failed to save screenshot: %w", err)
	}

	return nil
}
