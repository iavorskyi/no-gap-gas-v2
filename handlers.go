package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var screenshotsPath = "./data/screenshots"

// SetScreenshotsPath sets the base path for screenshots
func SetScreenshotsPath(path string) {
	screenshotsPath = path
}

// handleGetMe returns current user info
func handleGetMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	user, err := GetUserByID(userID)
	if err != nil || user == nil {
		jsonError(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UserResponse{
		ID:        user.ID,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	})
}

// ChangePasswordRequest is the request body for password change
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword handles password change
func handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < 6 {
		jsonError(w, "New password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	user, err := GetUserByID(userID)
	if err != nil || user == nil {
		jsonError(w, "User not found", http.StatusNotFound)
		return
	}

	if !VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		jsonError(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	if err := UpdateUserPassword(userID, req.NewPassword); err != nil {
		jsonError(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Password updated"})
}

// ConfigUpdateRequest is the request body for config update
type ConfigUpdateRequest struct {
	GasolinaEmail     string      `json:"gasolina_email"`
	GasolinaPassword  string      `json:"gasolina_password"`
	AccountNumber     string      `json:"account_number"`
	CheckURL          string      `json:"check_url"`
	CronSchedule      string      `json:"cron_schedule"`
	DryRun            *bool       `json:"dry_run"`
	MonthlyIncrements map[int]int `json:"monthly_increments"`
}

// handleGetConfig returns user's Gasolina config
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	cfg, err := GetUserConfig(userID)
	if err != nil {
		jsonError(w, "Failed to get config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

// handleUpdateConfig updates user's Gasolina config
func handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	var req ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get existing config for defaults
	existing, _ := GetUserConfig(userID)

	dryRun := existing.DryRun
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}

	if err := SaveUserConfig(
		userID,
		req.GasolinaEmail,
		req.GasolinaPassword,
		req.AccountNumber,
		req.CheckURL,
		req.CronSchedule,
		dryRun,
		req.MonthlyIncrements,
	); err != nil {
		jsonError(w, "Failed to update config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Configuration updated"})
}

// CreateJobRequest is the request body for creating a job
type CreateJobRequest struct {
	Type string `json:"type"`
}

// JobListResponse is the response for listing jobs
type JobListResponse struct {
	Jobs  []*Job `json:"jobs"`
	Total int    `json:"total"`
}

// handleCreateJob creates a new job
func handleCreateJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate job type
	validTypes := map[string]bool{"full": true, "test-login": true, "test-check": true}
	if !validTypes[req.Type] {
		jsonError(w, "Invalid job type. Must be 'full', 'test-login', or 'test-check'", http.StatusBadRequest)
		return
	}

	// Check user config
	cfg, err := GetUserConfig(userID)
	if err != nil {
		jsonError(w, "Failed to get user config", http.StatusInternalServerError)
		return
	}
	if !cfg.Configured {
		jsonError(w, "Gasolina credentials not configured. Please update your config first.", http.StatusBadRequest)
		return
	}

	// Create and queue job
	job, err := jobManager.CreateJob(userID, req.Type)
	if err != nil {
		jsonError(w, "Failed to create job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(job)
}

// handleListJobs lists user's jobs
func handleListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Parse query params
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	status := r.URL.Query().Get("status")

	jobs, total, err := GetUserJobs(userID, limit, status)
	if err != nil {
		jsonError(w, "Failed to get jobs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JobListResponse{Jobs: jobs, Total: total})
}

// JobDetailResponse is the detailed job response including screenshots
type JobDetailResponse struct {
	*Job
	Screenshots []*Screenshot `json:"screenshots,omitempty"`
}

// handleGetJob returns job details
func handleGetJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	job, err := GetJob(jobID)
	if err != nil {
		jsonError(w, "Failed to get job", http.StatusInternalServerError)
		return
	}
	if job == nil {
		jsonError(w, "Job not found", http.StatusNotFound)
		return
	}

	// Verify ownership
	if job.UserID != userID {
		jsonError(w, "Job not found", http.StatusNotFound)
		return
	}

	// Get screenshots
	screenshots, _ := GetJobScreenshots(jobID)
	for _, s := range screenshots {
		s.URL = fmt.Sprintf("/api/screenshots/%s/%s", jobID, s.Filename)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JobDetailResponse{Job: job, Screenshots: screenshots})
}

// handleListScreenshots lists screenshots for a job
func handleListScreenshots(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Verify job ownership
	job, err := GetJob(jobID)
	if err != nil || job == nil || job.UserID != userID {
		jsonError(w, "Job not found", http.StatusNotFound)
		return
	}

	screenshots, err := GetJobScreenshots(jobID)
	if err != nil {
		jsonError(w, "Failed to get screenshots", http.StatusInternalServerError)
		return
	}

	for _, s := range screenshots {
		s.URL = fmt.Sprintf("/api/screenshots/%s/%s", jobID, s.Filename)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(screenshots)
}

// handleGetScreenshot serves a screenshot file
func handleGetScreenshot(w http.ResponseWriter, r *http.Request, jobID, filename string) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Verify job ownership
	job, err := GetJob(jobID)
	if err != nil || job == nil || job.UserID != userID {
		jsonError(w, "Job not found", http.StatusNotFound)
		return
	}

	// Sanitize filename to prevent path traversal
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." {
		jsonError(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	// Construct file path
	filePath := filepath.Join(screenshotsPath, fmt.Sprintf("%d", userID), jobID, filename)

	// Check if file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) || info.IsDir() {
		jsonError(w, "Screenshot not found", http.StatusNotFound)
		return
	}

	// Open and serve file
	file, err := os.Open(filePath)
	if err != nil {
		jsonError(w, "Failed to open screenshot", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set content type based on extension
	contentType := "application/octet-stream"
	if strings.HasSuffix(filename, ".png") {
		contentType = "image/png"
	} else if strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg") {
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	io.Copy(w, file)
}

// handleHealth returns service health status
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleStatus returns service status (protected)
func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := GetUserIDFromContext(r.Context())
	if !ok {
		jsonError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	cfg, _ := GetUserConfig(userID)
	jobs, _, _ := GetUserJobs(userID, 5, "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"configured":  cfg.Configured,
		"recent_jobs": jobs,
	})
}
