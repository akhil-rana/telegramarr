package auth

import (
	"time"
)

type SessionData struct {
	UserID            int64     `json:"user_id"`
	PhoneNumber       string    `json:"phone_number"`
	FirstName         string    `json:"first_name"`
	LastName          string    `json:"last_name"`
	Username          string    `json:"username"`
	SessionCreated    time.Time `json:"session_created"`
	LastAuthenticated time.Time `json:"last_authenticated"`
	Authenticated     bool      `json:"authenticated"`
	Premium           bool      `json:"premium"`
	// Telegram session data (base64 encoded) - contains auth keys and tokens
	TelegramSession string `json:"telegram_session"`
}

type AuthRequest struct {
	Phone string `json:"phone_number"`
	Code  string `json:"code"`
	TwoFA string `json:"password"`
}
