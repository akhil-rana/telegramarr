package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"
	"time"

	qrcode "github.com/yeqown/go-qrcode/v2"
)

// AuthManager manages the authentication state machine
type AuthManager struct {
	mu              sync.RWMutex
	appID           int32
	appHash         string
	phone           string
	code            string
	password        string
	qrCodeData      []byte
	state           AuthState
	sessionData     *SessionData
	authFlowTimeout time.Duration
}

// AuthState represents the current authentication state
type AuthState int

const (
	AuthStateNone AuthState = iota
	AuthStateWaitingForPhone
	AuthStateWaitingForCode
	AuthStateWaitingFor2FA
	AuthStateAuthenticated
	AuthStateError
)

// NewAuthManager creates a new authentication manager
func NewAuthManager(appID int32, appHash string) *AuthManager {
	return &AuthManager{
		appID:           appID,
		appHash:         appHash,
		state:           AuthStateNone,
		authFlowTimeout: 5 * time.Minute,
	}
}

// StartQRAuth initiates QR code authentication
func (am *AuthManager) StartQRAuth(ctx context.Context) (string, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Generate QR code data
	qrData := fmt.Sprintf("tg://auth?bot_id=%d&hash=%s", am.appID, am.appHash)

	// Create QR code
	qr, err := qrcode.New(qrData)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}

	// Create a custom writer to render the QR code to an image and then PNG
	w := &pngWriter{buf: new(bytes.Buffer)}
	err = qr.Save(w)
	if err != nil {
		return "", fmt.Errorf("failed to encode QR image: %w", err)
	}

	// Convert to base64
	b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(w.buf.Bytes())

	am.qrCodeData = w.buf.Bytes()
	am.state = AuthStateWaitingForPhone

	return b64, nil
}

// pngWriter implements the Writer interface and renders QR code to PNG
type pngWriter struct {
	buf *bytes.Buffer
}

func (pw *pngWriter) Write(mat qrcode.Matrix) error {
	bitmap := mat.Bitmap()
	height := len(bitmap)
	width := len(bitmap[0])

	// Create image (scaled up for better visibility)
	scale := 4
	img := image.NewRGBA(image.Rect(0, 0, width*scale, height*scale))

	// Fill with white background
	for y := 0; y < height*scale; y++ {
		for x := 0; x < width*scale; x++ {
			img.Set(x, y, color.White)
		}
	}

	// Set black for QR code modules
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if bitmap[y][x] {
				// Draw scaled module
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						img.Set(x*scale+sx, y*scale+sy, color.Black)
					}
				}
			}
		}
	}

	// Encode to PNG
	return png.Encode(pw.buf, img)
}

func (pw *pngWriter) Close() error {
	return nil
}

// StartPhoneAuth initiates phone number authentication
func (am *AuthManager) StartPhoneAuth(ctx context.Context, phone string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.state != AuthStateNone && am.state != AuthStateError {
		return fmt.Errorf("authentication already in progress")
	}

	am.phone = phone
	am.state = AuthStateWaitingForCode

	return nil
}

// SubmitConfirmationCode submits the confirmation code received via SMS
func (am *AuthManager) SubmitConfirmationCode(ctx context.Context, code string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.state != AuthStateWaitingForCode {
		return fmt.Errorf("not waiting for confirmation code")
	}

	if len(code) < 4 {
		return fmt.Errorf("invalid confirmation code")
	}

	am.code = code
	am.state = AuthStateWaitingFor2FA

	return nil
}

// Submit2FAPassword submits the 2FA password
func (am *AuthManager) Submit2FAPassword(ctx context.Context, password string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.state != AuthStateWaitingFor2FA {
		return fmt.Errorf("not waiting for 2FA password")
	}

	if len(password) == 0 {
		return fmt.Errorf("password cannot be empty")
	}

	am.password = password

	// Create mock session
	am.sessionData = &SessionData{
		UserID:         123456789,
		PhoneNumber:    am.phone,
		FirstName:      "Test",
		LastName:       "User",
		Username:       "testuser",
		SessionCreated: time.Now(),
		Authenticated:  true,
	}

	am.state = AuthStateAuthenticated

	return nil
}

// Skip2FA skips 2FA if not required
func (am *AuthManager) Skip2FA(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.state != AuthStateWaitingFor2FA {
		return fmt.Errorf("not waiting for 2FA")
	}

	am.sessionData = &SessionData{
		UserID:         123456789,
		PhoneNumber:    am.phone,
		FirstName:      "Test",
		LastName:       "User",
		Username:       "testuser",
		SessionCreated: time.Now(),
		Authenticated:  true,
	}

	am.state = AuthStateAuthenticated

	return nil
}

// GetStatus returns the current authentication status
func (am *AuthManager) GetStatus() map[string]interface{} {
	am.mu.RLock()
	defer am.mu.RUnlock()

	status := map[string]interface{}{
		"state":            am.getStateName(),
		"authenticated":    am.state == AuthStateAuthenticated,
		"phone":            am.phone,
		"waiting_for_code": am.state == AuthStateWaitingForCode,
		"waiting_for_2fa":  am.state == AuthStateWaitingFor2FA,
	}

	if am.sessionData != nil {
		status["user"] = am.sessionData
	}

	return status
}

// GetSessionData returns the current session data if authenticated
func (am *AuthManager) GetSessionData() *SessionData {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if am.state != AuthStateAuthenticated {
		return nil
	}

	return am.sessionData
}

// Reset clears all authentication state
func (am *AuthManager) Reset() {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.phone = ""
	am.code = ""
	am.password = ""
	am.qrCodeData = nil
	am.sessionData = nil
	am.state = AuthStateNone
}

// getStateName returns the string representation of the current state
func (am *AuthManager) getStateName() string {
	states := map[AuthState]string{
		AuthStateNone:            "none",
		AuthStateWaitingForPhone: "waiting_for_phone",
		AuthStateWaitingForCode:  "waiting_for_code",
		AuthStateWaitingFor2FA:   "waiting_for_2fa",
		AuthStateAuthenticated:   "authenticated",
		AuthStateError:           "error",
	}
	return states[am.state]
}
