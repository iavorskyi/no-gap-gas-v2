package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB

// InitDB initializes the PostgreSQL database and runs migrations
func InitDB(databaseURL string) error {
	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	if err := runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// CloseDB closes the database connection
func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func runMigrations() error {
	migrations := []string{
		// Users table
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// User configurations (Gasolina credentials)
		`CREATE TABLE IF NOT EXISTS configs (
			id SERIAL PRIMARY KEY,
			user_id INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			gasolina_email TEXT,
			gasolina_password TEXT,
			account_number TEXT,
			check_url TEXT DEFAULT 'https://gasolina-online.com/indicator',
			cron_schedule TEXT DEFAULT '0 0 1 * *',
			dry_run BOOLEAN DEFAULT TRUE,
			monthly_increments TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Refresh tokens
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT UNIQUE NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Jobs table
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			type TEXT NOT NULL,
			status TEXT NOT NULL,
			error TEXT,
			logs TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ
		)`,

		// Screenshots table
		`CREATE TABLE IF NOT EXISTS screenshots (
			id SERIAL PRIMARY KEY,
			job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			filename TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Index for faster queries
		`CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON jobs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_screenshots_job_id ON screenshots(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// User represents a user in the system
type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserConfig represents a user's Gasolina configuration
type UserConfig struct {
	ID                int64       `json:"id"`
	UserID            int64       `json:"user_id"`
	GasolinaEmail     string      `json:"gasolina_email,omitempty"`
	GasolinaPassword  string      `json:"-"` // Never expose
	AccountNumber     string      `json:"account_number,omitempty"`
	CheckURL          string      `json:"check_url"`
	CronSchedule      string      `json:"cron_schedule"`
	DryRun            bool        `json:"dry_run"`
	MonthlyIncrements map[int]int `json:"monthly_increments,omitempty"`
	Configured        bool        `json:"configured"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

// Job represents a job execution record
type Job struct {
	ID          string     `json:"id"`
	UserID      int64      `json:"user_id"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	Error       *string    `json:"error,omitempty"`
	Logs        []string   `json:"logs,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Screenshot represents a screenshot record
type Screenshot struct {
	ID        int64     `json:"id"`
	JobID     string    `json:"job_id"`
	UserID    int64     `json:"user_id"`
	Filename  string    `json:"filename"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateUser creates a new user with hashed password
func CreateUser(email, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var id int64
	err = db.QueryRow(
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		email, string(hash),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return GetUserByID(id)
}

// GetUserByID retrieves a user by ID
func GetUserByID(id int64) (*User, error) {
	user := &User{}
	err := db.QueryRow(
		"SELECT id, email, password_hash, created_at, updated_at FROM users WHERE id = $1",
		id,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}

// GetUserByEmail retrieves a user by email
func GetUserByEmail(email string) (*User, error) {
	user := &User{}
	err := db.QueryRow(
		"SELECT id, email, password_hash, created_at, updated_at FROM users WHERE email = $1",
		email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}

// VerifyPassword checks if the provided password matches the hash
func VerifyPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// UpdateUserPassword updates a user's password
func UpdateUserPassword(userID int64, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	_, err = db.Exec(
		"UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2",
		string(hash), userID,
	)
	return err
}

// GetUserConfig retrieves a user's configuration
func GetUserConfig(userID int64) (*UserConfig, error) {
	cfg := &UserConfig{UserID: userID}
	var incrementsJSON sql.NullString
	var gasolinaEmail, gasolinaPassword, accountNumber, checkURL, cronSchedule sql.NullString

	err := db.QueryRow(`
		SELECT id, gasolina_email, gasolina_password, account_number, check_url,
		       cron_schedule, dry_run, monthly_increments, created_at, updated_at
		FROM configs WHERE user_id = $1`, userID,
	).Scan(&cfg.ID, &gasolinaEmail, &gasolinaPassword, &accountNumber,
		&checkURL, &cronSchedule, &cfg.DryRun, &incrementsJSON,
		&cfg.CreatedAt, &cfg.UpdatedAt)

	if err == sql.ErrNoRows {
		// Return default config
		return &UserConfig{
			UserID:       userID,
			CheckURL:     "https://gasolina-online.com/indicator",
			CronSchedule: "0 0 1 * *",
			DryRun:       true,
			Configured:   false,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	cfg.GasolinaEmail = gasolinaEmail.String
	cfg.AccountNumber = accountNumber.String

	// Apply defaults for empty values
	cfg.CheckURL = checkURL.String
	if cfg.CheckURL == "" {
		cfg.CheckURL = "https://gasolina-online.com/indicator"
	}
	cfg.CronSchedule = cronSchedule.String
	if cfg.CronSchedule == "" {
		cfg.CronSchedule = "0 0 1 * *"
	}

	// Decrypt password if present
	if gasolinaPassword.Valid && gasolinaPassword.String != "" {
		decrypted, err := decrypt(gasolinaPassword.String)
		if err == nil {
			cfg.GasolinaPassword = decrypted
		}
	}

	// Parse increments JSON
	if incrementsJSON.Valid && incrementsJSON.String != "" {
		if err := json.Unmarshal([]byte(incrementsJSON.String), &cfg.MonthlyIncrements); err != nil {
			cfg.MonthlyIncrements = make(map[int]int)
		}
	}

	cfg.Configured = cfg.GasolinaEmail != "" && cfg.GasolinaPassword != ""
	return cfg, nil
}

// SaveUserConfig saves or updates a user's configuration
func SaveUserConfig(userID int64, email, password, accountNumber, checkURL, cronSchedule string, dryRun bool, increments map[int]int) error {
	// Encrypt password if provided
	var encryptedPassword string
	if password != "" {
		var err error
		encryptedPassword, err = encrypt(password)
		if err != nil {
			return fmt.Errorf("failed to encrypt password: %w", err)
		}
	}

	// Serialize increments
	var incrementsJSON []byte
	if increments != nil {
		var err error
		incrementsJSON, err = json.Marshal(increments)
		if err != nil {
			return fmt.Errorf("failed to serialize increments: %w", err)
		}
	}

	// Upsert config
	_, err := db.Exec(`
		INSERT INTO configs (user_id, gasolina_email, gasolina_password, account_number,
		                     check_url, cron_schedule, dry_run, monthly_increments)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(user_id) DO UPDATE SET
			gasolina_email = COALESCE(NULLIF(excluded.gasolina_email, ''), configs.gasolina_email),
			gasolina_password = COALESCE(NULLIF(excluded.gasolina_password, ''), configs.gasolina_password),
			account_number = COALESCE(NULLIF(excluded.account_number, ''), configs.account_number),
			check_url = COALESCE(NULLIF(excluded.check_url, ''), configs.check_url),
			cron_schedule = COALESCE(NULLIF(excluded.cron_schedule, ''), configs.cron_schedule),
			dry_run = excluded.dry_run,
			monthly_increments = COALESCE(NULLIF(excluded.monthly_increments, ''), configs.monthly_increments),
			updated_at = NOW()`,
		userID, email, encryptedPassword, accountNumber, checkURL, cronSchedule, dryRun, string(incrementsJSON),
	)

	return err
}

// CreateJob creates a new job record
func CreateJob(id string, userID int64, jobType string) (*Job, error) {
	_, err := db.Exec(
		"INSERT INTO jobs (id, user_id, type, status) VALUES ($1, $2, $3, $4)",
		id, userID, jobType, "pending",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return GetJob(id)
}

// GetJob retrieves a job by ID
func GetJob(id string) (*Job, error) {
	job := &Job{}
	var errorStr, logsJSON sql.NullString
	var startedAt, completedAt sql.NullTime

	err := db.QueryRow(`
		SELECT id, user_id, type, status, error, logs, created_at, started_at, completed_at
		FROM jobs WHERE id = $1`, id,
	).Scan(&job.ID, &job.UserID, &job.Type, &job.Status, &errorStr, &logsJSON,
		&job.CreatedAt, &startedAt, &completedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	if errorStr.Valid {
		job.Error = &errorStr.String
	}
	if logsJSON.Valid {
		json.Unmarshal([]byte(logsJSON.String), &job.Logs)
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return job, nil
}

// GetUserJobs retrieves jobs for a user
func GetUserJobs(userID int64, limit int, status string) ([]*Job, int, error) {
	// Count total
	var total int
	var countQuery string
	var args []interface{}

	if status != "" {
		countQuery = "SELECT COUNT(*) FROM jobs WHERE user_id = $1 AND status = $2"
		args = []interface{}{userID, status}
	} else {
		countQuery = "SELECT COUNT(*) FROM jobs WHERE user_id = $1"
		args = []interface{}{userID}
	}

	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Query jobs
	var query string
	if status != "" {
		query = "SELECT id, user_id, type, status, error, logs, created_at, started_at, completed_at FROM jobs WHERE user_id = $1 AND status = $2 ORDER BY created_at DESC LIMIT $3"
		args = []interface{}{userID, status, limit}
	} else {
		query = "SELECT id, user_id, type, status, error, logs, created_at, started_at, completed_at FROM jobs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2"
		args = []interface{}{userID, limit}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		var errorStr, logsJSON sql.NullString
		var startedAt, completedAt sql.NullTime

		if err := rows.Scan(&job.ID, &job.UserID, &job.Type, &job.Status, &errorStr, &logsJSON,
			&job.CreatedAt, &startedAt, &completedAt); err != nil {
			return nil, 0, err
		}

		if errorStr.Valid {
			job.Error = &errorStr.String
		}
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, job)
	}

	return jobs, total, nil
}

// UpdateJobStatus updates a job's status
func UpdateJobStatus(id, status string, errorMsg *string) error {
	var err error
	if status == "running" {
		_, err = db.Exec(
			"UPDATE jobs SET status = $1, started_at = NOW() WHERE id = $2",
			status, id,
		)
	} else if status == "completed" || status == "failed" {
		_, err = db.Exec(
			"UPDATE jobs SET status = $1, error = $2, completed_at = NOW() WHERE id = $3",
			status, errorMsg, id,
		)
	} else {
		_, err = db.Exec("UPDATE jobs SET status = $1 WHERE id = $2", status, id)
	}
	return err
}

// AppendJobLogs appends logs to a job
func AppendJobLogs(id string, logs []string) error {
	logsJSON, _ := json.Marshal(logs)
	_, err := db.Exec("UPDATE jobs SET logs = $1 WHERE id = $2", string(logsJSON), id)
	return err
}

// CreateScreenshot creates a screenshot record
func CreateScreenshot(jobID string, userID int64, filename string) error {
	_, err := db.Exec(
		"INSERT INTO screenshots (job_id, user_id, filename) VALUES ($1, $2, $3)",
		jobID, userID, filename,
	)
	return err
}

// GetJobScreenshots retrieves screenshots for a job
func GetJobScreenshots(jobID string) ([]*Screenshot, error) {
	rows, err := db.Query(
		"SELECT id, job_id, user_id, filename, created_at FROM screenshots WHERE job_id = $1 ORDER BY created_at",
		jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var screenshots []*Screenshot
	for rows.Next() {
		s := &Screenshot{}
		if err := rows.Scan(&s.ID, &s.JobID, &s.UserID, &s.Filename, &s.CreatedAt); err != nil {
			return nil, err
		}
		screenshots = append(screenshots, s)
	}

	return screenshots, nil
}

// SaveRefreshToken saves a hashed refresh token
func SaveRefreshToken(userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := db.Exec(
		"INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)",
		userID, tokenHash, expiresAt,
	)
	return err
}

// GetRefreshToken retrieves a refresh token by hash
func GetRefreshToken(tokenHash string) (int64, time.Time, error) {
	var userID int64
	var expiresAt time.Time

	err := db.QueryRow(
		"SELECT user_id, expires_at FROM refresh_tokens WHERE token_hash = $1",
		tokenHash,
	).Scan(&userID, &expiresAt)

	if err == sql.ErrNoRows {
		return 0, time.Time{}, errors.New("token not found")
	}
	if err != nil {
		return 0, time.Time{}, err
	}

	return userID, expiresAt, nil
}

// DeleteRefreshToken deletes a refresh token
func DeleteRefreshToken(tokenHash string) error {
	_, err := db.Exec("DELETE FROM refresh_tokens WHERE token_hash = $1", tokenHash)
	return err
}

// DeleteUserRefreshTokens deletes all refresh tokens for a user
func DeleteUserRefreshTokens(userID int64) error {
	_, err := db.Exec("DELETE FROM refresh_tokens WHERE user_id = $1", userID)
	return err
}

// Encryption helpers using AES-256-GCM
var encryptionKey []byte

// SetEncryptionKey derives a 32-byte key from the JWT secret
func SetEncryptionKey(secret string) {
	hash := sha256.Sum256([]byte(secret))
	encryptionKey = hash[:]
}

func encrypt(plaintext string) (string, error) {
	if len(encryptionKey) == 0 {
		return "", errors.New("encryption key not set")
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(ciphertext string) (string, error) {
	if len(encryptionKey) == 0 {
		return "", errors.New("encryption key not set")
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:gcm.NonceSize()], string(data[gcm.NonceSize():])
	plaintext, err := gcm.Open(nil, nonce, []byte(ciphertext), nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
