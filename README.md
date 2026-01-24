# Gasolina Online Automation

Automated service for interacting with gasolina-online.com using browser automation and scheduled tasks.

## Features

- **Browser Automation**: Uses ChromeDP for headless browser automation
- **Scheduled Execution**: Runs automatically on a cron schedule (default: monthly)
- **Configurable**: All settings via environment variables
- **Retry Logic**: Automatic retries with exponential backoff
- **Debug Support**: Screenshot capture on errors
- **Test Modes**: Separate testing for login and checker functionality

## Prerequisites

- Go 1.24 or higher
- Chrome/Chromium browser installed (chromedp will use it)

## Installation

1. Clone the repository
2. Copy the example environment file:
   ```bash
   cp .env.example .env
   ```
3. Edit `.env` and fill in your credentials and configuration
4. Install dependencies:
   ```bash
   go mod download
   ```
5. Build the application:
   ```bash
   go build
   ```

## Configuration

Edit the `.env` file with your settings:

```env
GASOLINA_EMAIL=your-email@example.com
GASOLINA_PASSWORD=your-password
GASOLINA_CHECK_URL=https://gasolina-online.com/indicator
GASOLINA_MONTHLY_INCREMENTS={"1":50,"2":45,"3":50,"4":48,"5":50,"6":48,"7":50,"8":50,"9":48,"10":50,"11":48,"12":50}
CRON_SCHEDULE=0 0 1 * *
```

### Monthly Increments

The `GASOLINA_MONTHLY_INCREMENTS` is a JSON object where:
- **Key**: Month number (1-12, where 1=January, 2=February, etc.)
- **Value**: The increment to add to the current value for that month

Example:
```json
{"1":110, "2":100, "3":50, "4":30, "5":15, "6":15, "7":15, "8":15, "9":15, "10":50, "11":70, "12":100}
```

The application will:
1. Read the current value from the `#last_value` input on https://gasolina-online.com/ (e.g., 639)
2. Add the increment for the current month (e.g., 110 for January)
3. Submit the new calculated value (e.g., 749) on the indicator page

### Cron Schedule Format

Format: `minute hour day month day-of-week`

Examples:
- `0 0 1 * *` - Every 1st day of month at midnight (default)
- `0 9 * * 1` - Every Monday at 9am
- `0 0 15 * *` - Every 15th day of month at midnight
- `0 */6 * * *` - Every 6 hours

## How It Works

1. **Check Submission Window**: Verifies we're on days 1-5 of the month (the "Ввести" button is only enabled during this period)
2. **Login**: Authenticates to gasolina-online.com using provided credentials
3. **Read Current Value**: Navigates to main page and reads value from `#last_value` field
4. **Calculate New Value**: Adds the monthly increment to current value
5. **Navigate to Indicator Page**: Goes to the indicator page to check for existing records
6. **Check for Existing Record**: Searches for a record matching the current month/year in multiple formats:
   - Ukrainian month name format: "Січень 2026"
   - Numeric formats: "01.2026", "2026-01", "01/2026"
   - Short year format: "Січень 26"
7. **Skip if Exists**: If a record for the current month is found, the process stops (no duplicate entries)
8. **Prepare Submission**: If no record exists:
   - Finds the input field for entering the new value
   - Finds the "Ввести" button
   - Checks if the button is enabled
   - Logs exactly what it would do
   - **PERMANENTLY IN DRY-RUN MODE**: Does NOT actually submit (hardcoded safety feature)

**Important Notes**:
- The "Ввести" button is only active on days 1-5 of each month
- The application is **permanently in dry-run mode** - it will log all actions but NOT submit
- This is a safety feature to prevent accidental submissions
- All submission logic is implemented and ready, but disabled by the `DRY_RUN = true` constant in `checker.go:109`

### Enabling Real Submissions (Optional)

The application is currently in permanent dry-run mode for safety. To enable actual form submissions:

1. Open `checker.go`
2. Find line 109: `const DRY_RUN = true`
3. Change it to: `const DRY_RUN = false`
4. Rebuild: `go build`

**Warning**: Only do this after thoroughly testing in dry-run mode and verifying the logs show correct values!

## Usage

### Run with Scheduler

Start the service and wait for scheduled execution:

```bash
./my-go-service
```

### Run Immediately

Execute the job right now without waiting for the schedule:

```bash
./my-go-service --now
```

### Dry Run Mode

Test without actually submitting forms:

```bash
./my-go-service --now --dry-run
```

### Test Login Only

Test the login functionality:

```bash
./my-go-service --test-login
```

This will attempt to login and save a screenshot (`test_login_success.png` or `test_login_error.png`).

### Test Checker Only

Test the page checking and form filling logic:

```bash
./my-go-service --test-check
```

This will login, navigate to the target page, and perform checks without submitting (dry run mode).

## Development

### Build

```bash
go build
```

### Run

```bash
go run .
```

### Format Code

```bash
go fmt ./...
```

### Run Tests

```bash
go test ./...
```

## Deployment

### As a System Service (Linux)

Create a systemd service file at `/etc/systemd/system/gasolina-automation.service`:

```ini
[Unit]
Description=Gasolina Online Automation Service
After=network.target

[Service]
Type=simple
User=youruser
WorkingDirectory=/path/to/my-go-service
ExecStart=/path/to/my-go-service/my-go-service
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl enable gasolina-automation
sudo systemctl start gasolina-automation
sudo systemctl status gasolina-automation
```

### With Docker

Create a `Dockerfile`:

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o my-go-service

FROM chromedp/headless-shell:latest
COPY --from=builder /app/my-go-service /app/my-go-service
COPY .env /app/.env
WORKDIR /app
CMD ["./my-go-service"]
```

Build and run:

```bash
docker build -t gasolina-automation .
docker run -d --name gasolina-automation gasolina-automation
```

## Troubleshooting

### Screenshots

On errors, the application saves screenshots for debugging:
- `error_login.png` - Login error
- `error_check.png` - Checker error
- `error_<timestamp>.png` - Form submission error
- `test_login_success.png` - Successful login test
- `test_check_success.png` - Successful checker test

### Logs

The application logs all activities to stdout. To save logs to a file:

```bash
./my-go-service 2>&1 | tee gasolina.log
```

### Common Issues

1. **Chrome not found**: Install Chrome/Chromium on your system
2. **Login fails**: Check credentials in `.env` file
3. **Page not loading**: Increase sleep timeouts in `auth.go` and `checker.go`
4. **Form selectors not working**: The website structure may have changed - update selectors in `checker.go`
5. **Month detection not working**: The application checks for multiple date formats. If it doesn't detect existing records correctly, check the page content and update the patterns in `checkForCurrentMonthRecord()` function in `checker.go`
6. **Always creating duplicate records**: Check the logs to see which date format should be detected. You may need to add the specific format used by the website to the pattern list

## Architecture

- `main.go` - Entry point, scheduler, and job orchestration
- `config.go` - Configuration loading from environment variables
- `auth.go` - Login automation using chromedp
- `checker.go` - Page checking and form filling logic
- `.env` - Configuration file (not tracked in git)
- `.env.example` - Example configuration template

## Security

- Never commit the `.env` file to version control
- Keep your credentials secure
- Review the code before running to ensure it matches your expectations
- Use dry-run mode first to test without making changes

## License

MIT
