package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akhil-rana/telegramarr/internal/auth"
	"github.com/akhil-rana/telegramarr/internal/config"
	"github.com/akhil-rana/telegramarr/internal/pool"
	tgc "github.com/akhil-rana/telegramarr/internal/telegram"
	"github.com/akhil-rana/telegramarr/internal/tmdb"
	"github.com/akhil-rana/telegramarr/internal/uploader"
	"github.com/akhil-rana/telegramarr/internal/webhook"
	"github.com/gin-gonic/gin"
	"github.com/go-faster/errors"
	"github.com/gorilla/websocket"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	tgauth "github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	qrcode "github.com/yeqown/go-qrcode/v2"
	"go.uber.org/zap"
)

type Server struct {
	router        *gin.Engine
	logger        *zap.Logger
	config        *config.Config
	session       *auth.SessionData
	sessionPath   string
	sessionDBPath string
	dataDir       string
	tgClient      *telegram.Client
	clientManager *auth.ClientManager
	tmdbClient    *tmdb.Client
}

type SocketMessage struct {
	AuthType      string `json:"auth_type"`
	PhoneNo       string `json:"phone_no"`
	PhoneCode     string `json:"phone_code"`
	PhoneCodeHash string `json:"phone_code_hash"`
	Password      string `json:"password"`
	Message       string `json:"message"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func NewServer(logger *zap.Logger, cfg *config.Config, session *auth.SessionData, dataDir string) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	sessionPath := filepath.Join(dataDir, "session.json")
	sessionDBPath := filepath.Join(dataDir, "session.db")

	s := &Server{
		router:        router,
		logger:        logger,
		config:        cfg,
		session:       session,
		sessionPath:   sessionPath,
		sessionDBPath: sessionDBPath,
		dataDir:       dataDir,
	}

	// Initialize TMDB client if enabled and API key is provided
	if cfg.TMDB.Enabled && cfg.TMDB.APIKey != "" {
		s.tmdbClient = tmdb.NewClient(&cfg.TMDB, logger)
		logger.Info("TMDB client initialized")
	} else if cfg.TMDB.Enabled && cfg.TMDB.APIKey == "" {
		logger.Warn("TMDB is enabled but API key is not provided, disabling TMDB features")
	}

	// Initialize Telegram client with session if available and authenticated
	if session != nil && session.Authenticated {
		client, err := auth.CreateClientFromSession(context.Background(), cfg.Telegram.AppID, cfg.Telegram.AppHash, session, &cfg.Telegram, logger)
		if err != nil {
			logger.Error("Failed to create Telegram client from session", zap.Error(err))
		} else {
			s.tgClient = client

			// Create ClientManager to run client in background
			// This is CRITICAL - without this, the connection drops after ~1 minute
			s.clientManager = auth.NewClientManager(client, logger)

			// Wait for initial connection
			if err := s.clientManager.WaitForConnection(5 * time.Second); err != nil {
				logger.Error("Failed to connect Telegram client", zap.Error(err))
			} else {
				logger.Info("Telegram client connected successfully")
			}
		}
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Serve static files from React build output
	s.router.Static("/assets", "./src/ui/dist/assets")

	// Auth API endpoints
	auth := s.router.Group("/api/auth")
	{
		auth.GET("/status", s.AuthStatus)
		auth.GET("/ws", s.AuthWebSocket)
		auth.POST("/logout", s.Logout)
	}

	// Settings API
	settings := s.router.Group("/api/settings")
	{
		settings.GET("", s.GetSettings)
	}

	// Webhooks API
	webhooks := s.router.Group("/api/webhooks")
	{
		webhooks.POST("/radarr", s.RadarrWebhook)
		webhooks.POST("/sonarr", s.SonarrWebhook)
	}

	// Serve React app (catch-all for frontend routing)
	s.router.GET("/", s.ServeIndex)
	s.router.NoRoute(s.ServeIndex)
}

func (s *Server) Run(port string) error {
	s.logger.Info("Starting server on port " + port)
	return s.router.Run("0.0.0.0:" + port)
}

// Handlers
func (s *Server) AuthStatus(c *gin.Context) {
	if s.session != nil && s.session.Authenticated {
		c.JSON(200, gin.H{
			"authenticated": true,
			"user":          s.sessionToResponse(s.session),
			"state":         "authenticated",
		})
	} else {
		c.JSON(200, gin.H{
			"authenticated": false,
			"state":         "none",
		})
	}
}

// sessionToResponse converts SessionData to a response without sensitive fields
func (s *Server) sessionToResponse(session *auth.SessionData) gin.H {
	return gin.H{
		"user_id":            session.UserID,
		"phone_number":       session.PhoneNumber,
		"first_name":         session.FirstName,
		"last_name":          session.LastName,
		"username":           session.Username,
		"session_created":    session.SessionCreated,
		"last_authenticated": session.LastAuthenticated,
		"authenticated":      session.Authenticated,
		"premium":            session.Premium,
	}
}

func (s *Server) AuthWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade WebSocket", zap.Error(err))
		return
	}
	defer conn.Close()

	s.logger.Info("WebSocket connection established")

	ctx := c.Request.Context()

	// Create Telegram client with update dispatcher
	dispatcher := tg.NewUpdateDispatcher()
	sessionStorage := &session.StorageMemory{}

	tgClient, err := s.createTelegramClient(ctx, &dispatcher, sessionStorage)
	if err != nil {
		s.logger.Error("Failed to create Telegram client", zap.Error(err))
		conn.WriteJSON(map[string]any{"type": "error", "message": "Failed to initialize client"})
		return
	}

	err = tgClient.Run(ctx, func(ctx context.Context) error {
		for {
			msg := &SocketMessage{}
			err := conn.ReadJSON(msg)
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					s.logger.Info("WebSocket closed by client")
					return nil
				}
				s.logger.Error("Failed to read message", zap.Error(err))
				return err
			}

			s.logger.Info("Received auth message", zap.String("auth_type", msg.AuthType))

			switch msg.AuthType {
			case "qr":
				s.logger.Info("Starting QR auth")
				go s.handleQRAuth(ctx, conn, tgClient, &dispatcher, sessionStorage)
			case "phone":
				s.logger.Info("Starting phone auth", zap.String("message", msg.Message))
				go s.handlePhoneAuth(ctx, conn, tgClient, msg, sessionStorage)
			case "2fa":
				if msg.Password != "" {
					s.logger.Info("Starting 2FA auth")
					go s.handle2FAAuth(ctx, conn, tgClient, msg.Password, sessionStorage)
				}
			default:
				s.logger.Warn("Unknown auth type", zap.String("auth_type", msg.AuthType))
				conn.WriteJSON(map[string]any{"type": "error", "message": "unknown auth type"})
			}
		}
	})

	if err != nil {
		s.logger.Error("WebSocket error", zap.Error(err))
	}
}

func (s *Server) createTelegramClient(ctx context.Context, dispatcher *tg.UpdateDispatcher, sessionStorage session.Storage) (*telegram.Client, error) {
	// Create middlewares for the client
	// Order: FloodWait -> Recovery -> Retry -> RateLimit
	// Use background context for recovery to avoid cancellation during RPC calls
	bgCtx := context.Background()
	middlewares := tgc.NewMiddleware(&s.config.Telegram,
		tgc.WithFloodWait(),
		tgc.WithRecovery(bgCtx),
		tgc.WithRetry(s.config.Telegram.MaxRetries),
		tgc.WithRateLimit(),
	)

	// Create client with device info and middleware chain
	return telegram.NewClient(
		s.config.Telegram.AppID,
		s.config.Telegram.AppHash,
		telegram.Options{
			SessionStorage: sessionStorage,
			UpdateHandler:  dispatcher,
			Middlewares:    middlewares,
			Device: telegram.DeviceConfig{
				DeviceModel:    s.config.Telegram.DeviceModel,
				SystemVersion:  s.config.Telegram.SystemVersion,
				AppVersion:     s.config.Telegram.AppVersion,
				SystemLangCode: s.config.Telegram.SystemLangCode,
				LangPack:       s.config.Telegram.LangPack,
				LangCode:       s.config.Telegram.LangCode,
			},
		},
	), nil
}

func (s *Server) handleQRAuth(ctx context.Context, conn *websocket.Conn, tgClient *telegram.Client, dispatcher *tg.UpdateDispatcher, sessionStorage session.Storage) {
	s.logger.Info("QR auth: Starting QR authentication")
	loggedIn := qrlogin.OnLoginToken(dispatcher)

	authorization, err := tgClient.QR().Auth(ctx, loggedIn, func(ctx context.Context, token qrlogin.Token) error {
		qrURL := token.URL()
		s.logger.Info("QR auth: Generated token", zap.String("url", qrURL))

		// Generate QR code image and send to frontend
		qrCodeData, err := s.generateQRCode(qrURL)
		if err != nil {
			s.logger.Error("Failed to generate QR code image", zap.Error(err))
			conn.WriteJSON(map[string]any{"type": "error", "message": "failed to generate QR code"})
			return err
		}

		// Send QR code to frontend for display - using "auth" type like teldrive
		conn.WriteJSON(map[string]any{
			"type":    "auth",
			"payload": map[string]string{"token": qrCodeData},
		})
		return nil
	})

	// Check if context was canceled
	if errors.Is(err, context.Canceled) {
		s.logger.Info("QR auth: Context canceled")
		return
	}

	// Check for 2FA requirement
	if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		s.logger.Info("QR auth: 2FA required")
		conn.WriteJSON(map[string]any{"type": "auth", "message": "2FA required"})
		return
	}

	if err != nil {
		s.logger.Error("QR auth failed", zap.Error(err))
		conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}

	s.logger.Info("QR auth: No error, getting user")
	user, ok := authorization.User.AsNotEmpty()
	if !ok {
		s.logger.Error("QR auth: User is empty")
		conn.WriteJSON(map[string]any{"type": "error", "message": "auth failed"})
		return
	}

	s.logger.Info("QR auth: User authenticated", zap.String("username", user.Username))

	sessionDataObj, err := s.createAndSaveSession(ctx, conn, user, sessionStorage)
	if err != nil {
		s.logger.Error("QR auth: Failed to create/save session", zap.Error(err))
		conn.WriteJSON(map[string]any{"type": "error", "message": "failed to save session"})
		return
	}

	s.session = sessionDataObj

	s.logger.Info("QR auth: Sending success message with payload")
	conn.WriteJSON(map[string]any{
		"type":    "auth",
		"payload": s.sessionToResponse(sessionDataObj),
		"message": "success",
	})
	s.logger.Info("QR auth: Success message sent")
}

// generateQRCode converts a string to a base64-encoded PNG QR code
func (s *Server) generateQRCode(data string) (string, error) {
	qr, err := qrcode.New(data)
	if err != nil {
		return "", err
	}

	// Create a buffer writer
	buf := new(bytes.Buffer)
	w := &bufferWriter{buf}
	err = qr.Save(w)
	if err != nil {
		return "", err
	}

	// Convert to base64 data URL
	b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	return b64, nil
}

// bufferWriter implements the Writer interface for bytes.Buffer
type bufferWriter struct {
	buf *bytes.Buffer
}

func (bw *bufferWriter) Write(mat qrcode.Matrix) error {
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
	return png.Encode(bw.buf, img)
}

func (bw *bufferWriter) Close() error {
	return nil
}

func (s *Server) handlePhoneAuth(ctx context.Context, conn *websocket.Conn, tgClient *telegram.Client, msg *SocketMessage, sessionStorage session.Storage) {
	switch msg.Message {
	case "sendcode":
		res, err := tgClient.Auth().SendCode(ctx, msg.PhoneNo, tgauth.SendCodeOptions{})
		if errors.Is(err, context.Canceled) {
			return
		}

		if err != nil {
			s.logger.Error("Failed to send code", zap.Error(err))
			conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}

		code := res.(*tg.AuthSentCode)
		conn.WriteJSON(map[string]any{
			"type":    "auth",
			"payload": map[string]string{"phoneCodeHash": code.PhoneCodeHash},
		})

	case "signin":
		authResult, err := tgClient.Auth().SignIn(ctx, msg.PhoneNo, msg.PhoneCode, msg.PhoneCodeHash)
		if errors.Is(err, context.Canceled) {
			return
		}

		if errors.Is(err, tgauth.ErrPasswordAuthNeeded) {
			s.logger.Info("Phone auth: 2FA required")
			conn.WriteJSON(map[string]any{"type": "auth", "message": "2FA required"})
			return
		}

		if tgerr.Is(err, "PHONE_CODE_INVALID") {
			s.logger.Error("Phone code invalid", zap.Error(err))
			conn.WriteJSON(map[string]any{"type": "auth", "message": "PHONE_CODE_INVALID"})
			return
		}

		if err != nil {
			s.logger.Error("Sign in failed", zap.Error(err))
			conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
			return
		}

		user, ok := authResult.User.AsNotEmpty()
		if !ok {
			conn.WriteJSON(map[string]any{"type": "error", "message": "auth failed"})
			return
		}

		s.logger.Info("Phone auth: User authenticated", zap.String("username", user.Username))

		sessionDataObj, err := s.createAndSaveSession(ctx, conn, user, sessionStorage)
		if err != nil {
			s.logger.Error("Phone auth: Failed to create/save session", zap.Error(err))
			conn.WriteJSON(map[string]any{"type": "error", "message": "failed to save session"})
			return
		}

		s.session = sessionDataObj

		conn.WriteJSON(map[string]any{
			"type":    "auth",
			"payload": s.sessionToResponse(sessionDataObj),
			"message": "success",
		})
	}
}

func (s *Server) handle2FAAuth(ctx context.Context, conn *websocket.Conn, tgClient *telegram.Client, password string, sessionStorage session.Storage) {
	authResult, err := tgClient.Auth().Password(ctx, password)
	if errors.Is(err, context.Canceled) {
		return
	}

	if err != nil {
		s.logger.Error("2FA failed", zap.Error(err))
		conn.WriteJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}

	user, ok := authResult.User.AsNotEmpty()
	if !ok {
		conn.WriteJSON(map[string]any{"type": "error", "message": "auth failed"})
		return
	}

	s.logger.Info("2FA auth: User authenticated", zap.String("username", user.Username))

	sessionDataObj, err := s.createAndSaveSession(ctx, conn, user, sessionStorage)
	if err != nil {
		s.logger.Error("2FA auth: Failed to create/save session", zap.Error(err))
		conn.WriteJSON(map[string]any{"type": "error", "message": "failed to save session"})
		return
	}

	s.session = sessionDataObj

	conn.WriteJSON(map[string]any{
		"type":    "auth",
		"payload": s.sessionToResponse(sessionDataObj),
		"message": "success",
	})
}

// createAndSaveSession consolidates session creation logic following DRY principle
// It loads the Telegram session, encodes it, and saves it to disk.
// This ensures session tokens are persisted immediately after successful authentication.
func (s *Server) createAndSaveSession(ctx context.Context, conn *websocket.Conn, user *tg.User, sessionStorage session.Storage) (*auth.SessionData, error) {
	// Load Telegram session data from storage
	tgSessionData, err := sessionStorage.LoadSession(ctx)
	if err != nil {
		s.logger.Error("Failed to load telegram session", zap.Error(err))
		return nil, err
	}

	// Encode session as base64
	tgSessionB64 := base64.StdEncoding.EncodeToString(tgSessionData)

	// Create session with authenticated user data
	now := time.Now()
	sessionDataObj := &auth.SessionData{
		UserID:            user.ID,
		PhoneNumber:       user.Phone,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		Username:          user.Username,
		Authenticated:     true,
		Premium:           user.Premium,
		TelegramSession:   tgSessionB64,
		SessionCreated:    now,
		LastAuthenticated: now,
	}

	s.logger.Info("Session object created", zap.String("user", user.Username))
	s.logger.Info("Saving session to path", zap.String("path", s.sessionPath))

	if err := auth.SaveSession(s.sessionPath, sessionDataObj); err != nil {
		s.logger.Error("Failed to save session", zap.Error(err))
		return nil, err
	}

	s.logger.Info("Session saved successfully")

	// Note: If this session is used later for operations, wrap it with PersistentSessionStorage
	// to ensure token updates are auto-saved. For now, just save the initial session.

	return sessionDataObj, nil
}

func (s *Server) Logout(c *gin.Context) {
	if err := auth.DeleteSession(s.sessionPath, s.sessionDBPath); err != nil {
		s.logger.Error("Failed to delete session", zap.Error(err))
		c.JSON(500, gin.H{"error": "failed to logout"})
		return
	}

	s.session = nil

	c.JSON(200, gin.H{
		"status":  "logged_out",
		"message": "Successfully logged out",
	})
}

func (s *Server) GetSettings(c *gin.Context) {
	if s.session == nil || !s.session.Authenticated {
		c.JSON(401, gin.H{"error": "not authenticated"})
		return
	}

	c.JSON(200, gin.H{
		"app_id":         s.config.Telegram.AppID,
		"radarr_channel": s.config.Telegram.RadarrChannelID,
		"sonarr_channel": s.config.Telegram.SonarrChannelID,
		"username":       s.session.Username,
	})
}

func (s *Server) RadarrWebhook(c *gin.Context) {
	var payload webhook.RadarrWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		s.logger.Error("Failed to parse Radarr webhook", zap.Error(err))
		c.JSON(400, gin.H{"error": "invalid payload"})
		return
	}

	// Log entire JSON payload for debugging/testing
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	s.logger.Info("Radarr webhook JSON",
		zap.String("payload", string(payloadJSON)))

	// Accept test events from Radarr (they have eventType: "Test")
	if payload.EventType == "Test" {
		s.logger.Info("Radarr test webhook received")
		c.JSON(200, gin.H{
			"message": "Test webhook received successfully",
			"status":  "ok",
		})
		return
	}

	// Validate real payload
	if !webhook.ValidateRadarrPayload(&payload) {
		s.logger.Warn("Invalid Radarr payload - missing required fields")
		c.JSON(400, gin.H{"error": "invalid or missing required fields"})
		return
	}

	s.logger.Info("Radarr webhook received",
		zap.String("movie", payload.Movie.Title),
		zap.Int("year", payload.Movie.Year),
		zap.String("imdbId", payload.Movie.ImdbID),
		zap.Int("tmdbId", payload.Movie.TmdbID))

	// Process webhook asynchronously
	go s.processRadarrWebhook(&payload)

	c.JSON(202, gin.H{
		"message": "Radarr webhook queued for processing",
		"movie":   payload.Movie.Title,
		"status":  "processing",
	})
}

func (s *Server) SonarrWebhook(c *gin.Context) {
	var payload webhook.SonarrWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		s.logger.Error("Failed to parse Sonarr webhook", zap.Error(err))
		c.JSON(400, gin.H{"error": "invalid payload"})
		return
	}

	// Log entire JSON payload for debugging/testing
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	s.logger.Info("Sonarr webhook JSON",
		zap.String("payload", string(payloadJSON)))

	// Accept test events from Sonarr (they have eventType: "Test")
	if payload.EventType == "Test" {
		s.logger.Info("Sonarr test webhook received")
		c.JSON(200, gin.H{
			"message": "Test webhook received successfully",
			"status":  "ok",
		})
		return
	}

	// Validate real payload
	if !webhook.ValidateSonarrPayload(&payload) {
		s.logger.Warn("Invalid Sonarr payload - missing required fields")
		c.JSON(400, gin.H{"error": "invalid or missing required fields"})
		return
	}

	s.logger.Info("Sonarr webhook received",
		zap.String("series", payload.Series.Title),
		zap.Int("year", payload.Series.Year),
		zap.String("imdbId", payload.Series.ImdbID),
		zap.Int("tvdbId", payload.Series.TvdbID))

	// Process webhook asynchronously
	go s.processSonarrWebhook(&payload)

	c.JSON(202, gin.H{
		"message": "Sonarr webhook queued for processing",
		"series":  payload.Series.Title,
		"status":  "processing",
	})
}

// Helper functions for formatting messages

// formatProgressBar creates a visual progress bar
func formatProgressBar(percent int) string {
	const totalBars = 15
	filled := (percent * totalBars) / 100
	empty := totalBars - filled

	var bar strings.Builder
	for i := 0; i < filled; i++ {
		bar.WriteString("█")
	}
	for i := 0; i < empty; i++ {
		bar.WriteString("░")
	}
	return bar.String()
}

// formatBytes converts bytes to human readable format matching Telegram's display
// Uses: bytes ÷ (1024*1024) ÷ 1000 which is equivalent to bytes ÷ (1048576 * 1000)
// This gives: 4,194,304,000 bytes → 4.0 GB (exactly matches Telegram)
func formatBytes(bytes int64) string {
	const mib = 1024 * 1024 // 1 MiB in bytes
	const gib = mib * 1000  // 1 GiB in Telegram's display (1024 MiB × 1000)

	if bytes >= gib {
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(gib))
	}
	// For MB display, use 1 decimal
	return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mib))
}

// formatSpeed calculates and formats speed in MBps using 1024*1000 formula (matches file display units)
func formatSpeed(bytes int64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return "0.0 MBps"
	}
	mbps := float64(bytes) / (1024 * 1000) / elapsed.Seconds()
	return fmt.Sprintf("%.1f MBps", mbps)
}

// formatSpeedDelta calculates and formats speed based on bytes in a time delta (for current speed)
func formatSpeedDelta(bytes int64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return "0.0 MBps"
	}
	mbps := float64(bytes) / (1024 * 1000) / elapsed.Seconds()
	return fmt.Sprintf("%.1f MBps", mbps)
}

// formatRARProgressMessage creates formatted RAR splitting message (kept for backward compatibility)
func formatRARProgressMessage(fileName string, source string, title string, percent int, bytesProcessed int64, totalBytes int64) string {
	return formatArchiveProgressMessage(fileName, source, title, "rar", percent, bytesProcessed, totalBytes)
}

// formatArchiveProgressMessage creates formatted archive splitting message for RAR or 7z
func formatArchiveProgressMessage(fileName string, source string, title string, format string, percent int, bytesProcessed int64, totalBytes int64) string {
	bar := formatProgressBar(percent)
	currentMB := formatBytes(bytesProcessed)
	totalMB := formatBytes(totalBytes)

	formatName := strings.ToUpper(format)
	if format == "7z" {
		formatName = "7z"
	}

	return fmt.Sprintf(
		"<b>📦 %s Webhook Upload</b>\n"+
			"\n"+
			"<b>Title:</b> %s\n"+
			"<b>File:</b> <b><code>%s</code></b>\n"+
			"\n"+
			"<b>Preparing %s Parts</b>\n"+
			"%s %d%%\n"+
			"\n"+
			"<b>Progress:</b> %s / %s",
		strings.ToUpper(source), title, fileName, formatName, bar, percent, currentMB, totalMB,
	)
}

// formatUploadProgressMessage creates formatted upload message with speed
func formatUploadProgressMessage(fileNum int, totalFiles int, source string, title string, fileName string, percent int, current int64, total int64, elapsed time.Duration) string {
	bar := formatProgressBar(percent)
	currentMB := formatBytes(current)
	totalMB := formatBytes(total)
	speed := formatSpeed(current, elapsed)

	// Calculate ETA
	var eta string
	if current > 0 && elapsed > 0 {
		totalSeconds := float64(total) * elapsed.Seconds() / float64(current)
		remainingSeconds := totalSeconds - elapsed.Seconds()
		if remainingSeconds > 0 {
			minutes := int(remainingSeconds) / 60
			seconds := int(remainingSeconds) % 60
			if minutes == 0 {
				eta = fmt.Sprintf("<b>ETA:</b> %ds", seconds)
			} else if seconds == 0 {
				eta = fmt.Sprintf("<b>ETA:</b> %dm", minutes)
			} else {
				eta = fmt.Sprintf("<b>ETA:</b> %dm%ds", minutes, seconds)
			}
		}
	}

	return fmt.Sprintf(
		"📤 <b>Uploading Part %d of %d</b>\n"+
			"\n"+
			"<b>Source:</b> %s\n"+
			"<b>Title:</b> %s\n"+
			"<b>File:</b> <b><code>%s</code></b>\n"+
			"\n"+
			"%s %d%%\n"+
			"\n"+
			"<b>Progress:</b> %s / %s\n"+
			"<b>Speed:</b> %s\n"+
			"%s",
		fileNum, totalFiles, strings.ToUpper(source), title, fileName, bar, percent, currentMB, totalMB, speed, eta,
	)
}

// formatUploadProgressMessageWithSpeed creates formatted upload message with pre-calculated speed
func formatUploadProgressMessageWithSpeed(fileNum int, totalFiles int, source string, title string, fileName string, percent int, current int64, total int64, elapsed time.Duration, speed string) string {
	bar := formatProgressBar(percent)
	currentMB := formatBytes(current)
	totalMB := formatBytes(total)

	// Calculate ETA
	var eta string
	if current > 0 && elapsed > 0 {
		totalSeconds := float64(total) * elapsed.Seconds() / float64(current)
		remainingSeconds := totalSeconds - elapsed.Seconds()
		if remainingSeconds > 0 {
			minutes := int(remainingSeconds) / 60
			seconds := int(remainingSeconds) % 60
			if minutes == 0 {
				eta = fmt.Sprintf("<b>ETA:</b> %ds", seconds)
			} else if seconds == 0 {
				eta = fmt.Sprintf("<b>ETA:</b> %dm", minutes)
			} else {
				eta = fmt.Sprintf("<b>ETA:</b> %dm%ds", minutes, seconds)
			}
		}
	}

	return fmt.Sprintf(
		"📤 <b>Uploading Part %d of %d</b>\n"+
			"\n"+
			"<b>Source:</b> %s\n"+
			"<b>Title:</b> %s\n"+
			"<b>File:</b> <b><code>%s</code></b>\n"+
			"\n"+
			"%s %d%%\n"+
			"\n"+
			"<b>Progress:</b> %s / %s\n"+
			"<b>Speed:</b> %s\n"+
			"%s",
		fileNum, totalFiles, strings.ToUpper(source), title, fileName, bar, percent, currentMB, totalMB, speed, eta,
	)
}

func (s *Server) ServeIndex(c *gin.Context) {
	c.File("./src/ui/dist/index.html")
}

// processRadarrWebhook processes the Radarr webhook and uploads the movie file
func (s *Server) processRadarrWebhook(payload *webhook.RadarrWebhookPayload) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Radarr webhook goroutine panicked", zap.Any("panic", r))
		}
	}()

	ctx := context.Background()

	s.logger.Info("Processing Radarr webhook",
		zap.String("movie", payload.Movie.Title),
		zap.String("imdbId", payload.Movie.ImdbID),
		zap.String("filePath", payload.MovieFile.Path))

	// Apply delay to allow filesystem to catch up (especially for rclone mounts)
	delayTime := time.Duration(s.config.Telegram.DelayTime) * time.Second
	s.logger.Info("Radarr webhook: waiting before processing",
		zap.Duration("delay", delayTime))
	time.Sleep(delayTime)

	// Verify file exists
	if _, err := os.Stat(payload.MovieFile.Path); err != nil {
		s.logger.Error("Radarr file does not exist", zap.Error(err), zap.String("path", payload.MovieFile.Path))
		return
	}

	// Check if client is ready
	if s.clientManager == nil || s.tgClient == nil {
		s.logger.Error("Telegram client not initialized - cannot upload Radarr webhook")
		return
	}

	if !s.clientManager.IsConnected() {
		s.logger.Error("Telegram client is not connected")
		return
	}

	// Process file upload with metadata for filename shortening
	s.uploadWebhookFile(ctx, payload.MovieFile.Path, payload.Movie.Title, payload.Movie.ImdbID, payload.Movie.TmdbID, "radarr", s.config.Telegram.RadarrChannelID, &uploader.ShortenerConfig{
		MovieTitle:   payload.Movie.Title,
		Quality:      payload.MovieFile.Quality,
		Format:       payload.MovieFile.MediaInfo.VideoCodec,
		ReleaseGroup: payload.MovieFile.ReleaseGroup,
	})
}

// processSonarrWebhook processes the Sonarr webhook and uploads the episode file
func (s *Server) processSonarrWebhook(payload *webhook.SonarrWebhookPayload) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Sonarr webhook goroutine panicked", zap.Any("panic", r))
		}
	}()

	ctx := context.Background()

	s.logger.Info("Processing Sonarr webhook",
		zap.String("series", payload.Series.Title),
		zap.String("imdbId", payload.Series.ImdbID),
		zap.Int("tvdbId", payload.Series.TvdbID),
		zap.String("filePath", payload.EpisodeFile.Path))

	// Apply delay to allow filesystem to catch up (especially for rclone mounts)
	delayTime := time.Duration(s.config.Telegram.DelayTime) * time.Second
	s.logger.Info("Sonarr webhook: waiting before processing",
		zap.Duration("delay", delayTime))
	time.Sleep(delayTime)

	// Verify file exists
	if _, err := os.Stat(payload.EpisodeFile.Path); err != nil {
		s.logger.Error("Sonarr file does not exist", zap.Error(err), zap.String("path", payload.EpisodeFile.Path))
		return
	}

	// Check if client is ready
	if s.clientManager == nil || s.tgClient == nil {
		s.logger.Error("Telegram client not initialized - cannot upload Sonarr webhook")
		return
	}

	if !s.clientManager.IsConnected() {
		s.logger.Error("Telegram client is not connected")
		return
	}

	// Process file upload
	s.uploadWebhookFile(ctx, payload.EpisodeFile.Path, payload.Series.Title, payload.Series.ImdbID, payload.Series.TvdbID, "sonarr", s.config.Telegram.SonarrChannelID, nil)
}

// sendTMDBMovieInfo fetches movie details from TMDB and sends poster image with info to Telegram
// Retries up to 5 times if API fails, uses exponential backoff between retries
func (s *Server) sendTMDBMovieInfo(ctx context.Context, tmdbID int, title string, source string, channelID int64) {
	maxRetries := 5
	var movieDetails *tmdb.MovieDetails
	var err error

	// Retry logic for fetching TMDB movie details with custom backoff intervals
	backoffIntervals := []time.Duration{5 * time.Second, 8 * time.Second, 15 * time.Second, 20 * time.Second, 30 * time.Second}
	for attempt := 1; attempt <= maxRetries; attempt++ {
		movieDetails, err = s.tmdbClient.GetMovieDetails(ctx, tmdbID)
		if err == nil {
			s.logger.Info("Fetched TMDB movie details", zap.String("title", movieDetails.Title), zap.Int("id", movieDetails.ID))
			break
		}

		s.logger.Warn("Failed to fetch TMDB movie details, retrying...",
			zap.Error(err),
			zap.Int("tmdb_id", tmdbID),
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", maxRetries))

		// Don't sleep after last failed attempt
		if attempt < maxRetries {
			backoffDuration := backoffIntervals[attempt-1]
			s.logger.Debug("Waiting before retry", zap.Duration("duration", backoffDuration), zap.Int("attempt", attempt))
			time.Sleep(backoffDuration)
		}
	}

	// If all retries failed, log error and return
	if err != nil {
		s.logger.Error("Failed to fetch TMDB movie details after all retries",
			zap.Error(err),
			zap.Int("tmdb_id", tmdbID),
			zap.Int("attempts", maxRetries))
		return
	}

	// Create pool for message sending
	poolSize := int64(s.config.Telegram.PoolSize)
	if poolSize < 1 {
		poolSize = 8
	}

	middlewares := tgc.NewMiddleware(
		&s.config.Telegram,
		tgc.WithFloodWait(),
		tgc.WithRecovery(ctx),
		tgc.WithRetry(s.config.Telegram.MaxRetries),
		tgc.WithRateLimit(),
	)

	uploadPool := pool.NewPool(s.tgClient, poolSize, s.logger, middlewares...)
	defer uploadPool.Close()

	messenger := uploader.NewChannelMessenger(s.tgClient, channelID, s.logger, uploadPool)

	// Send poster image with movie details if available
	if movieDetails.PosterPath != "" {
		posterURL := s.tmdbClient.GetPosterURL(movieDetails.PosterPath)

		// Retry logic for downloading poster image with custom backoff intervals
		var posterBytes []byte
		backoffIntervals := []time.Duration{5 * time.Second, 8 * time.Second, 15 * time.Second, 20 * time.Second, 30 * time.Second}
		for attempt := 1; attempt <= maxRetries; attempt++ {
			posterBytes, err = s.tmdbClient.DownloadImage(ctx, posterURL)
			if err == nil {
				s.logger.Info("Downloaded poster image successfully", zap.String("url", posterURL))
				break
			}

			s.logger.Warn("Failed to download poster image, retrying...",
				zap.Error(err),
				zap.String("url", posterURL),
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", maxRetries))

			// Don't sleep after last failed attempt
			if attempt < maxRetries {
				backoffDuration := backoffIntervals[attempt-1]
				s.logger.Debug("Waiting before retry", zap.Duration("duration", backoffDuration), zap.Int("attempt", attempt))
				time.Sleep(backoffDuration)
			}
		}

		// If poster download failed after retries, log and continue (don't block file upload)
		if err != nil {
			s.logger.Warn("Failed to download poster after all retries, continuing without poster",
				zap.Error(err),
				zap.String("url", posterURL),
				zap.Int("attempts", maxRetries))
		} else {
			// Format movie details for caption
			caption := tmdb.FormatMovieDetailsMessage(movieDetails)

			// Send photo with caption
			if _, err := messenger.SendPhotoFromBytes(ctx, posterBytes, caption); err != nil {
				s.logger.Error("Failed to send movie poster", zap.Error(err), zap.String("title", movieDetails.Title))
			} else {
				s.logger.Info("Sent movie poster to channel", zap.String("title", movieDetails.Title))
			}
		}
	} else {
		s.logger.Info("No poster path available in TMDB data", zap.String("title", movieDetails.Title))
	}
}

// uploadWebhookFile handles the common file upload logic for both Radarr and Sonarr webhooks
// metadata: optional ShortenerConfig for RAR filename shortening (only used for RAR splits)
func (s *Server) uploadWebhookFile(ctx context.Context, filePath string, title string, imdbIDOrTvdbString interface{}, tmdbIDOrTvdbInt interface{}, source string, channelID int64, metadata *uploader.ShortenerConfig) {
	// Check file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		s.logger.Error("File does not exist", zap.Error(err), zap.String("path", filePath), zap.String("source", source))
		return
	}

	fileSize := fileInfo.Size()
	isPremium := s.session != nil && s.session.Premium

	// Extract TMDB ID if available (tmdbIDOrTvdbInt should be int for Radarr)
	tmdbID := 0
	if id, ok := tmdbIDOrTvdbInt.(int); ok && id > 0 {
		tmdbID = id
	}

	// Fetch and send TMDB movie details with poster if available
	if tmdbID > 0 && s.tmdbClient != nil && source == "radarr" {
		s.sendTMDBMovieInfo(ctx, tmdbID, title, source, channelID)
	}

	// Determine size limit based on premium status
	var sizeLimit int64
	if isPremium {
		sizeLimit = 4000000000 // 4GB for premium
	} else {
		sizeLimit = 2000000000 // 2GB for free
	}

	// If file is smaller than limit, upload directly without RAR
	if fileSize < sizeLimit {
		s.uploadWebhookFileDirectly(ctx, filePath, title, source, fileSize, channelID)
		return
	}

	// File is larger than limit, use RAR splitting
	s.uploadWebhookFileWithRar(ctx, filePath, title, source, isPremium, sizeLimit, channelID, metadata)
}

// uploadWebhookFileDirectly uploads a file without RAR splitting
func (s *Server) uploadWebhookFileDirectly(ctx context.Context, filePath string, title string, source string, fileSize int64, channelID int64) {
	s.logger.Info("Uploading file directly (no RAR needed)", zap.String("file", filePath), zap.Int64("size", fileSize), zap.String("source", source))

	isPremium := s.session != nil && s.session.Premium

	// Send filename caption before uploading
	fileName := filepath.Base(filePath)
	caption := fmt.Sprintf("<code>%s</code>", fileName)

	// Create a temporary messenger to send the caption
	poolSize := int64(s.config.Telegram.PoolSize)
	if poolSize < 1 {
		poolSize = 8
	}

	middlewares := tgc.NewMiddleware(
		&s.config.Telegram,
		tgc.WithFloodWait(),
		tgc.WithRecovery(ctx),
		tgc.WithRetry(s.config.Telegram.MaxRetries),
		tgc.WithRateLimit(),
	)

	uploadPool := pool.NewPool(s.tgClient, poolSize, s.logger, middlewares...)
	defer uploadPool.Close()

	messenger := uploader.NewChannelMessenger(s.tgClient, channelID, s.logger, uploadPool)
	if _, err := messenger.SendMessage(ctx, caption); err != nil {
		s.logger.Warn("Failed to send filename caption", zap.Error(err), zap.String("file", fileName))
	}

	// Use centralized upload mechanism
	if err := s.uploadFilesToChannel(ctx, []string{filePath}, title, source, isPremium, channelID); err != nil {
		s.logger.Error("File upload failed", zap.Error(err), zap.String("source", source))
	}

	s.logger.Info("Webhook file upload complete", zap.String("source", source), zap.String("title", title), zap.String("file", fileName))
}

// uploadWebhookFileWithRar uploads a large file using archive splitting (RAR or 7z) via centralized upload mechanism
// metadata: optional ShortenerConfig for archive filename shortening
func (s *Server) uploadWebhookFileWithRar(ctx context.Context, filePath string, title string, source string, isPremium bool, sizeLimit int64, channelID int64, metadata *uploader.ShortenerConfig) {
	// Create messenger for archive progress updates
	poolSize := int64(s.config.Telegram.PoolSize)
	if poolSize < 1 {
		poolSize = 8
	}

	middlewares := tgc.NewMiddleware(
		&s.config.Telegram,
		tgc.WithFloodWait(),
		tgc.WithRecovery(ctx),
		tgc.WithRetry(s.config.Telegram.MaxRetries),
		tgc.WithRateLimit(),
	)

	uploadPool := pool.NewPool(s.tgClient, poolSize, s.logger, middlewares...)
	defer uploadPool.Close()

	messenger := uploader.NewChannelMessenger(s.tgClient, channelID, s.logger, uploadPool)

	// Send initial message
	fileName := filepath.Base(filePath)
	startMsg := fmt.Sprintf(
		"<b>📦 %s Webhook Upload</b>\n\n"+
			"<b>Title:</b> %s\n"+
			"<b>File:</b> %s\n\n"+
			"<b>Preparing Archive Parts...</b>",
		strings.ToUpper(source), title, fileName,
	)

	s.logger.Info("Sending initial webhook message to channel", zap.String("title", title), zap.String("source", source))
	msgID, err := messenger.SendMessage(ctx, startMsg)
	if err != nil {
		s.logger.Error("Failed to send initial webhook message", zap.Error(err), zap.String("source", source))
		return
	}

	// Split file with progress callback using configured archive format
	archiver := uploader.NewArchiver(s.config.Telegram.SplitArchiveFormat, s.logger)

	// Create temp directory at project root using the config helper
	tempArchiveDir, err := config.GetTempDir()
	if err != nil {
		s.logger.Error("Failed to get temp directory", zap.Error(err))
		updateMsg := fmt.Sprintf("❌ <b>Error getting temp directory:</b> %v", err)
		messenger.UpdateMessage(ctx, msgID, updateMsg)
		return
	}

	s.logger.Info("Starting archive split for webhook", zap.String("file", filePath), zap.String("source", source), zap.String("format", s.config.Telegram.SplitArchiveFormat))

	// Track last update time for progress messages (update interval from config)
	refreshInterval := time.Duration(s.config.Telegram.MessageRefreshInterval) * time.Second
	s.logger.Info("Message refresh interval set", zap.Duration("interval", refreshInterval), zap.Int("config_value", s.config.Telegram.MessageRefreshInterval))
	lastUpdateTime := time.Now()
	lastInfoLogTime := time.Now()
	var lastArchiveProgressMsg string

	// Split file with progress callback for Telegram updates
	archiveParts, err := archiver.SplitFile(filePath, tempArchiveDir, 0, isPremium, func(bytesProcessed, totalBytes int64, percent int) {
		// Only log periodically (every 10%) to avoid flooding logs
		if percent%10 == 0 {
			s.logger.Debug("Webhook archive split progress", zap.Int64("bytes", bytesProcessed), zap.Int64("total_bytes", totalBytes), zap.Int("percent", percent))
		}

		// Log at Info level only every 5 seconds (not on every callback)
		now := time.Now()
		if now.Sub(lastInfoLogTime) >= 5*time.Second && percent > 0 && percent < 100 {
			s.logger.Info("Webhook archive split in progress", zap.Int("percent", percent), zap.Int64("bytes", bytesProcessed), zap.String("source", source))
			lastInfoLogTime = now
		}

		// Edit status message at configured interval or at completion
		if now.Sub(lastUpdateTime) >= refreshInterval || percent == 100 {
			s.logger.Debug("Updating archive progress message", zap.Int("percent", percent), zap.Duration("time_since_update", now.Sub(lastUpdateTime)), zap.Duration("refresh_interval", refreshInterval))
			lastUpdateTime = now
			progressMsg := formatArchiveProgressMessage(fileName, source, title, s.config.Telegram.SplitArchiveFormat, percent, bytesProcessed, totalBytes)

			// Only update if message content actually changed
			if progressMsg == lastArchiveProgressMsg {
				return
			}
			lastArchiveProgressMsg = progressMsg

			// Edit the initial message
			err := messenger.UpdateMessage(context.Background(), msgID, progressMsg)
			if err != nil {
				s.logger.Error("Failed to update webhook archive status message", zap.Error(err), zap.Int("msg_id", msgID), zap.String("source", source))
				return
			}
			s.logger.Info("Webhook archive status message edited", zap.Int("msg_id", msgID), zap.Int("percent", percent))
		}
	}, metadata)

	if err != nil {
		s.logger.Error("Failed to split file for webhook", zap.Error(err), zap.String("source", source))
		// Delete initial message on error
		if msgID > 0 {
			if err := messenger.DeleteMessage(context.Background(), msgID); err != nil {
				s.logger.Warn("Failed to delete webhook initial message on error", zap.Error(err), zap.Int("msg_id", msgID))
			}
		}
		errorMsg := fmt.Sprintf("❌ <b>Error creating archive parts:</b> %v", err)
		messenger.SendMessage(ctx, errorMsg)
		return
	}

	if len(archiveParts) == 0 {
		s.logger.Warn("No archive parts created for webhook", zap.String("file", filePath), zap.String("source", source))
		if msgID > 0 {
			messenger.DeleteMessage(context.Background(), msgID)
		}
		return
	}

	s.logger.Info("File split complete for webhook", zap.Int("parts", len(archiveParts)), zap.String("source", source))

	// Use centralized upload mechanism for archive parts, passing the existing message ID to reuse it
	if err := s.uploadFilesToChannel(ctx, archiveParts, title, source, isPremium, channelID, msgID); err != nil {
		s.logger.Error("Failed to upload webhook archive parts", zap.Error(err), zap.String("source", source))
	}

	// Cleanup archive files
	archiver.CleanupFiles(archiveParts)
	s.logger.Info("Webhook upload complete", zap.String("source", source), zap.String("title", title))
}

// uploadFilesToChannel is the centralized upload mechanism used by both test and webhook uploads
// It handles pool creation, and actual file uploads using the already-authenticated client
// channelID: telegram channel to upload to
// existingMsgID: if provided (>0), will reuse that message for the first upload instead of creating a new one
func (s *Server) uploadFilesToChannel(ctx context.Context, filePaths []string, title string, source string, isPremium bool, channelID int64, existingMsgID ...int) error {
	// Ensure client is ready
	if s.clientManager == nil || s.tgClient == nil {
		s.logger.Error("Telegram client not initialized", zap.String("source", source))
		return fmt.Errorf("telegram client not initialized")
	}

	if !s.clientManager.IsConnected() {
		s.logger.Error("Telegram client is not connected", zap.String("source", source))
		return fmt.Errorf("telegram client not connected")
	}

	// Use the already-running client (already authenticated via main client manager)
	telegramClient := s.tgClient

	// Create connection pool with middlewares
	poolSize := int64(s.config.Telegram.PoolSize)
	if poolSize < 1 {
		poolSize = 8
	}

	middlewares := tgc.NewMiddleware(
		&s.config.Telegram,
		tgc.WithFloodWait(),
		tgc.WithRecovery(ctx),
		tgc.WithRetry(s.config.Telegram.MaxRetries),
		tgc.WithRateLimit(),
	)

	uploadPool := pool.NewPool(telegramClient, poolSize, s.logger, middlewares...)
	defer uploadPool.Close()

	// Create messenger and uploader
	messenger := uploader.NewChannelMessenger(telegramClient, channelID, s.logger, uploadPool)
	telegramUploader := uploader.NewTelegramUploader(telegramClient, channelID, s.logger, &s.config.Telegram, uploadPool)

	// Upload each file
	for i, filePath := range filePaths {
		fileName := filepath.Base(filePath)
		fileNum := i + 1
		totalFiles := len(filePaths)

		s.logger.Info("Uploading file", zap.String("file", fileName), zap.Int("number", fileNum), zap.Int("total", totalFiles), zap.String("source", source))

		// Determine message ID: use existing message for first file (if provided), create new for subsequent files
		var msgID int
		if i == 0 && len(existingMsgID) > 0 && existingMsgID[0] > 0 {
			// Reuse the existing message from RAR progress
			msgID = existingMsgID[0]
		} else {
			// Create a new status message for this file
			statusMsg := fmt.Sprintf(
				"📤 <b>Uploading Part %d of %d</b>\n\n"+
					"<b>Source:</b> %s\n"+
					"<b>Title:</b> %s\n"+
					"<b>File:</b> <b><code>%s</code></b>\n\n"+
					"<b>Starting...</b>",
				fileNum, totalFiles, strings.ToUpper(source), title, fileName,
			)
			msgID, _ = messenger.SendMessage(ctx, statusMsg)
		}

		// Track last update time for progress messages (update interval configurable from config)
		refreshInterval := time.Duration(s.config.Telegram.MessageRefreshInterval) * time.Second
		lastUpdateTime := time.Now()
		uploadStartTime := time.Now()
		lastInfoLogTime := time.Now() // Track Info-level logging (every 5 seconds)
		var lastProgressMsg string

		// Track bytes and time for current speed calculation (last 3 seconds)
		lastSpeedCheckTime := uploadStartTime
		lastSpeedCheckBytes := int64(0)

		// Upload the file with throttled progress updates and speed calculation
		_, err := telegramUploader.UploadToChannel(ctx, filePath, fileName, func(current, total int64, percent int) {
			elapsed := time.Since(uploadStartTime)
			now := time.Now()

			// Calculate current speed (last 3 seconds)
			var currentSpeed string
			timeSinceLastCheck := now.Sub(lastSpeedCheckTime).Seconds()
			if timeSinceLastCheck >= 3.0 || percent == 100 {
				// Update speed calculation
				bytesSinceLastCheck := current - lastSpeedCheckBytes
				if timeSinceLastCheck > 0 {
					currentSpeed = formatSpeedDelta(bytesSinceLastCheck, time.Duration(int64(timeSinceLastCheck*1000))*time.Millisecond)
				} else {
					currentSpeed = "0.0 MBps"
				}
				lastSpeedCheckTime = now
				lastSpeedCheckBytes = current
			} else {
				// Not enough time passed, use overall average for now
				currentSpeed = formatSpeed(current, elapsed)
			}

			// Log at Info level only every 5 seconds (not on every callback)
			if now.Sub(lastInfoLogTime) >= 5*time.Second && percent > 0 && percent < 100 {
				s.logger.Info("Upload in progress", zap.Int("part", fileNum), zap.Int("percent", percent), zap.String("source", source), zap.String("speed", currentSpeed))
				lastInfoLogTime = now
			}

			// Edit progress message at configured interval or at completion
			if msgID > 0 && (now.Sub(lastUpdateTime) >= refreshInterval || percent == 100) {
				lastUpdateTime = now
				progressMsg := formatUploadProgressMessageWithSpeed(fileNum, totalFiles, source, title, fileName, percent, current, total, elapsed, currentSpeed)

				// Only update if message content actually changed
				if progressMsg == lastProgressMsg {
					return
				}
				lastProgressMsg = progressMsg

				messenger.UpdateMessage(ctx, msgID, progressMsg)
			}
		})

		if err != nil {
			s.logger.Error("Failed to upload file", zap.Error(err), zap.String("file", fileName), zap.String("source", source))
			if msgID > 0 {
				errorMsg := fmt.Sprintf("❌ <b>Failed to upload Part %d:</b> %v", fileNum, err)
				messenger.UpdateMessage(ctx, msgID, errorMsg)
			}
			return err
		}

		// Delete progress message after successful upload
		if msgID > 0 {
			messenger.DeleteMessage(ctx, msgID)
		}

		time.Sleep(500 * time.Millisecond)
	}

	return nil
}
