package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

// Login performs authentication on gasolina-online.com
func Login(ctx context.Context, email, password, accountNumber string) error {
	log.Printf("Attempting to login as %s, psw: %s...", email, password)

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
	_ = SaveScreenshot(ctx, "debug_before_login.png")
	log.Println("Screenshot saved: debug_before_login.png")

	// Check what elements are on the page
	var pageHTML string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.documentElement.outerHTML`, &pageHTML),
	)
	if err != nil {
		log.Printf("Warning: couldn't get page HTML: %v", err)
	} else {
		log.Printf("Page HTML length: %d characters", len(pageHTML))
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
			log.Printf("Email field found with selector: %s", selector)
			emailFound = true
			break
		}
	}

	if !emailFound {
		return fmt.Errorf("email field not found - check debug_before_login.png")
	}

	// Try each password selector
	passwordFound := false
	for _, selector := range passwordSelectors {
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(selector, chromedp.ByQuery),
			chromedp.SendKeys(selector, password, chromedp.ByQuery),
		)
		if err == nil {
			log.Printf("Password field found with selector: %s", selector)
			passwordFound = true
			break
		}
	}

	if !passwordFound {
		return fmt.Errorf("password field not found - check debug_before_login.png")
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
			log.Printf("Login button found with selector: %s", selector)
			buttonFound = true
			break
		}
	}

	if !buttonFound {
		log.Println("Warning: login button not found, trying to submit form with Enter key")
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
	_ = SaveScreenshot(ctx, "debug_after_login.png")
	log.Println("Screenshot saved: debug_after_login.png")

	// Select account from dropdown
	if accountNumber != "" {
		log.Printf("Selecting account containing: %s", accountNumber)

		// First, click the hamburger menu to open navigation using JavaScript
		err = chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('.navbar-toggler').click()`, nil),
			chromedp.Sleep(1*time.Second),
		)
		if err != nil {
			log.Printf("Hamburger menu click failed: %v", err)
		}

		_ = SaveScreenshot(ctx, "debug_menu_open.png")
		log.Println("Screenshot saved: debug_menu_open.png")

		// Click the account dropdown toggle button using JavaScript
		err = chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('#dropdown01').click()`, nil),
			chromedp.Sleep(1*time.Second),
		)
		if err != nil {
			return fmt.Errorf("failed to click account dropdown: %w", err)
		}

		_ = SaveScreenshot(ctx, "debug_dropdown_open.png")
		log.Println("Screenshot saved: debug_dropdown_open.png")

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

		_ = SaveScreenshot(ctx, "debug_account_selected.png")
		log.Printf("Account %s selected successfully", accountNumber)
	}

	log.Println("Login sequence completed")
	return nil
}

// SaveScreenshot saves a screenshot for debugging purposes
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
