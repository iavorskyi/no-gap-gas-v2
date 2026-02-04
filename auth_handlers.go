package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecret       []byte
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

// SetJWTConfig configures JWT settings
func SetJWTConfig(secret string, accessTTL, refreshTTL time.Duration) {
	jwtSecret = []byte(secret)
	if accessTTL > 0 {
		accessTokenTTL = accessTTL
	}
	if refreshTTL > 0 {
		refreshTokenTTL = refreshTTL
	}
}

// Claims for JWT tokens
type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

// RegisterRequest is the request body for registration
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest is the request body for login
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RefreshRequest is the request body for token refresh
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// TokenResponse is the response containing tokens
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
}

// UserResponse is the response containing user info
type UserResponse struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// handleRegister handles user registration
func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		jsonError(w, "Invalid email address", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 6 {
		jsonError(w, "Password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	// Check if user exists
	existing, err := GetUserByEmail(req.Email)
	if err != nil {
		jsonError(w, "Database error", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		jsonError(w, "Email already registered", http.StatusConflict)
		return
	}

	// Create user
	user, err := CreateUser(req.Email, req.Password)
	if err != nil {
		jsonError(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(UserResponse{
		ID:        user.ID,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	})
}

// handleLogin handles user login
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	// Find user
	user, err := GetUserByEmail(req.Email)
	if err != nil {
		jsonError(w, "Database error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		jsonError(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	// Verify password
	if !VerifyPassword(user.PasswordHash, req.Password) {
		jsonError(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	// Generate tokens
	accessToken, err := generateAccessToken(user.ID)
	if err != nil {
		jsonError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	refreshToken, err := generateRefreshToken(user.ID)
	if err != nil {
		jsonError(w, "Failed to generate refresh token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
	})
}

// handleRefresh handles token refresh
func handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RefreshToken == "" {
		jsonError(w, "Refresh token required", http.StatusBadRequest)
		return
	}

	// Hash the token to look it up
	tokenHash := hashToken(req.RefreshToken)

	// Find token
	userID, expiresAt, err := GetRefreshToken(tokenHash)
	if err != nil {
		jsonError(w, "Invalid refresh token", http.StatusUnauthorized)
		return
	}

	// Check expiration
	if time.Now().After(expiresAt) {
		DeleteRefreshToken(tokenHash)
		jsonError(w, "Refresh token expired", http.StatusUnauthorized)
		return
	}

	// Generate new access token
	accessToken, err := generateAccessToken(userID)
	if err != nil {
		jsonError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken: accessToken,
		ExpiresIn:   int(accessTokenTTL.Seconds()),
	})
}

// handleLogout handles user logout
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RefreshToken != "" {
		tokenHash := hashToken(req.RefreshToken)
		DeleteRefreshToken(tokenHash)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Logged out"})
}

// generateAccessToken creates a new JWT access token
func generateAccessToken(userID int64) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// generateRefreshToken creates a new refresh token and stores its hash
func generateRefreshToken(userID int64) (string, error) {
	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Store hash in database
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(refreshTokenTTL)

	if err := SaveRefreshToken(userID, tokenHash, expiresAt); err != nil {
		return "", err
	}

	return token, nil
}

// hashToken creates a SHA256 hash of a token
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// parseAccessToken validates and parses an access token
func parseAccessToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

// jsonError sends a JSON error response
func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
