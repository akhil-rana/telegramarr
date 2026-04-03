package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/akhil-rana/telegramarr/internal/auth"
	"github.com/akhil-rana/telegramarr/internal/config"
	"github.com/akhil-rana/telegramarr/internal/pool"
	tgc "github.com/akhil-rana/telegramarr/internal/telegram"
	"github.com/akhil-rana/telegramarr/internal/uploader"
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
		webhooks.POST("/test-upload", s.TestUploadWebhook)
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
		"app_id":     s.config.Telegram.AppID,
		"channel_id": s.config.Telegram.ChannelID,
		"username":   s.session.Username,
	})
}

func (s *Server) RadarrWebhook(c *gin.Context) {
	c.JSON(202, gin.H{"message": "Queued for processing"})
}

func (s *Server) SonarrWebhook(c *gin.Context) {
	c.JSON(202, gin.H{"message": "Queued for processing"})
}

func (s *Server) ServeIndex(c *gin.Context) {
	c.File("./src/ui/dist/index.html")
}

// TestUploadWebhook is a test endpoint for uploading a movie file from test/movies folder
// Called from frontend when user clicks "Test Upload" button after authentication
func (s *Server) TestUploadWebhook(c *gin.Context) {
	if s.session == nil || !s.session.Authenticated {
		c.JSON(401, gin.H{"error": "not authenticated"})
		return
	}

	// Find first movie file in test/movies directory
	testMoviesDir := "test/movies"
	entries, err := os.ReadDir(testMoviesDir)
	if err != nil {
		s.logger.Error("Failed to read test movies directory", zap.Error(err), zap.String("path", testMoviesDir))
		c.JSON(400, gin.H{"error": "test movies directory not found"})
		return
	}

	var testFilePath string
	for _, entry := range entries {
		if !entry.IsDir() {
			testFilePath = filepath.Join(testMoviesDir, entry.Name())
			break
		}
	}

	if testFilePath == "" {
		s.logger.Error("No movie files found in test/movies directory")
		c.JSON(400, gin.H{"error": "no movie files found in test/movies directory"})
		return
	}

	// Verify file exists and is readable
	if _, err := os.Stat(testFilePath); err != nil {
		s.logger.Error("Test file not accessible", zap.Error(err), zap.String("path", testFilePath))
		c.JSON(400, gin.H{"error": "test file not accessible"})
		return
	}

	// Process upload asynchronously
	go s.processTestUpload(testFilePath)

	c.JSON(202, gin.H{
		"message": "Upload started",
		"status":  "processing",
		"file":    testFilePath,
	})
}

// processTestUpload handles the actual file upload with progress tracking in Telegram
func (s *Server) processTestUpload(filePath string) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Upload goroutine panicked", zap.Any("panic", r))
		}
	}()

	ctx := context.Background()

	s.logger.Info("Starting test upload", zap.String("file", filePath), zap.String("user", s.session.Username))

	// Check file exists
	if _, err := os.Stat(filePath); err != nil {
		s.logger.Error("Test file does not exist", zap.Error(err), zap.String("path", filePath))
		return
	}
	s.logger.Info("Test file found", zap.String("path", filePath))

	isPremium := s.session != nil && s.session.Premium
	s.logger.Info("Premium status", zap.Bool("isPremium", isPremium))

	// Use the already-running client from ClientManager
	// This is CRITICAL - the client must have client.Run() running in background
	// to avoid "context cancelled" errors during long uploads
	if s.clientManager == nil || s.tgClient == nil {
		s.logger.Error("Telegram client not initialized - cannot upload")
		return
	}

	// Ensure client is connected
	if !s.clientManager.IsConnected() {
		s.logger.Error("Telegram client is not connected")
		return
	}

	telegramClient := s.tgClient
	s.logger.Info("Using existing Telegram client for upload")

	// Run the client in a goroutine to keep it connected
	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()

	clientDone := make(chan error, 1)
	go func() {
		s.logger.Info("Starting telegram client runner")
		err := telegramClient.Run(clientCtx, func(ctx context.Context) error {
			s.logger.Info("Telegram client connected and ready")
			<-ctx.Done()
			return ctx.Err()
		})
		clientDone <- err
		s.logger.Info("Telegram client runner finished", zap.Error(err))
	}()

	// Give the client a moment to connect
	time.Sleep(2 * time.Second)

	// Create connection pool for messenger and uploader
	poolSize := int64(s.config.Telegram.PoolSize)
	if poolSize < 1 {
		poolSize = 8
	}

	// Create middleware chain for pool: FloodWait -> Recovery -> Retry -> RateLimit
	middlewares := tgc.NewMiddleware(
		&s.config.Telegram,
		tgc.WithFloodWait(),
		tgc.WithRecovery(ctx),
		tgc.WithRetry(s.config.Telegram.MaxRetries),
		tgc.WithRateLimit(),
	)

	uploadPool := pool.NewPool(telegramClient, poolSize, s.logger, middlewares...)
	defer uploadPool.Close()

	// Create messenger for sending updates to channel
	messenger := uploader.NewChannelMessenger(telegramClient, s.config.Telegram.ChannelID, s.logger, uploadPool)
	s.logger.Info("Messenger created", zap.Int64("channelID", s.config.Telegram.ChannelID))

	// Send initial message to channel
	fileName := filepath.Base(filePath)
	startMsg := fmt.Sprintf("🎬 **Upload Started**\nFile: %s\nPremium: %v\nPreparing RAR parts...", fileName, isPremium)

	s.logger.Info("Sending initial message to channel", zap.String("message", startMsg))
	msgID, err := messenger.SendMessage(ctx, startMsg)
	if err != nil {
		s.logger.Error("Failed to send initial message", zap.Error(err))
		return
	}

	s.logger.Info("Initial message sent to channel", zap.Int("msg_id", msgID))

	// Create RAR splitter
	rarSplitter := uploader.NewRarSplitter(s.logger)

	// Determine part size based on premium status
	partSize := uploader.NonPremiumMaxSize
	if isPremium {
		partSize = uploader.PremiumMaxSize
	}

	// Create temp directory for RAR files at project root if it doesn't exist
	tempRarDir := filepath.Join(".", "temp")
	if err := os.MkdirAll(tempRarDir, 0755); err != nil {
		s.logger.Error("Failed to create temp RAR directory", zap.Error(err))
		updateMsg := fmt.Sprintf("❌ Error creating temp directory: %v", err)
		messenger.UpdateMessage(ctx, msgID, updateMsg)
		return
	}

	s.logger.Info("Starting RAR split", zap.String("file", filePath), zap.Int64("part_size", partSize))

	// Track last update time for progress messages (update every 2 seconds)
	lastUpdateTime := time.Now()
	lastInfoLogTime := time.Now() // Track Info-level logging (every 5 seconds)
	var lastRarProgressMsg string

	rarParts, err := rarSplitter.SplitFile(filePath, tempRarDir, partSize, isPremium, func(bytesProcessed, totalBytes int64, percent int) {
		// Only log periodically (every 10%) to avoid flooding logs
		if percent%10 == 0 {
			s.logger.Debug("RAR split progress", zap.Int64("bytes", bytesProcessed), zap.Int64("total_bytes", totalBytes), zap.Int("percent", percent))
		}

		// Log at Info level only every 5 seconds (not on every callback)
		now := time.Now()
		if now.Sub(lastInfoLogTime) >= 5*time.Second && percent > 0 && percent < 100 {
			s.logger.Info("RAR split in progress", zap.Int("percent", percent), zap.Int64("bytes", bytesProcessed))
			lastInfoLogTime = now
		}

		// Edit status message every 2 seconds or at completion
		if now.Sub(lastUpdateTime) >= 2*time.Second || percent == 100 {
			lastUpdateTime = now
			progressMsg := fmt.Sprintf("🎬 **Preparing RAR Parts**\nFile: %s\nProgress: %d%%\nBytes: %.1f MB / %.1f MB", fileName, percent, float64(bytesProcessed)/1024/1024, float64(totalBytes)/1024/1024)

			// Only update if message content actually changed
			if progressMsg == lastRarProgressMsg {
				// Message content is the same, skip update to avoid MESSAGE_NOT_MODIFIED error
				return
			}
			lastRarProgressMsg = progressMsg

			// Always edit the initial message (msgID was sent at the start)
			err := messenger.UpdateMessage(context.Background(), msgID, progressMsg)
			if err != nil {
				s.logger.Error("Failed to update RAR status message", zap.Error(err), zap.Int("msg_id", msgID))
				return
			}
			s.logger.Info("RAR status message edited", zap.Int("msg_id", msgID))
		}
	})
	if err != nil {
		s.logger.Error("Failed to split file into RAR parts", zap.Error(err))
		// Delete initial message on error
		if msgID > 0 {
			if err := messenger.DeleteMessage(context.Background(), msgID); err != nil {
				s.logger.Warn("Failed to delete initial message on error", zap.Error(err), zap.Int("msg_id", msgID))
			}
		}
		errorMsg := fmt.Sprintf("❌ Error creating RAR parts: %v", err)
		messenger.SendMessage(ctx, errorMsg)
		return
	}

	// Delete initial message after RAR is done
	if msgID > 0 {
		if err := messenger.DeleteMessage(context.Background(), msgID); err != nil {
			s.logger.Warn("Failed to delete initial message", zap.Error(err), zap.Int("msg_id", msgID))
		}
	}

	s.logger.Info("RAR split complete", zap.Int("parts", len(rarParts)))

	// Create telegram uploader for sending files
	telegramUploader := uploader.NewTelegramUploader(telegramClient, s.config.Telegram.ChannelID, s.logger, &s.config.Telegram, uploadPool)
	s.logger.Info("Telegram uploader created")

	// Upload each RAR part sequentially to Telegram channel
	uploadedParts := 0

	for i, rarFilePath := range rarParts {
		partNum := i + 1
		zipFileInfo, err := os.Stat(rarFilePath)
		if err != nil {
			s.logger.Error("Failed to stat RAR file", zap.String("file", rarFilePath), zap.Error(err))
			continue
		}

		s.logger.Info("Uploading RAR part", zap.Int("number", partNum), zap.Int("total", len(rarParts)), zap.Int64("size", zipFileInfo.Size()))

		rarFileName := filepath.Base(rarFilePath)

		// Use context.Background() with timeout
		uploadCtx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)

		// Track last update time for progress updates (every 2 seconds)
		partLastUpdateTime := time.Now()
		partLastInfoLogTime := time.Now() // Track Info-level logging (every 5 seconds)
		var partStatusMsgID int
		var lastPartProgressMsg string

		_, err = telegramUploader.UploadToChannel(uploadCtx, rarFilePath, rarFileName, func(current, total int64, percent int) {
			// Calculate overall progress
			totalSize := int64(0)
			for _, rarPath := range rarParts {
				info, _ := os.Stat(rarPath)
				if info != nil {
					totalSize += info.Size()
				}
			}

			totalUploadedSoFar := int64(0)
			for j := 0; j < i; j++ {
				info, _ := os.Stat(rarParts[j])
				if info != nil {
					totalUploadedSoFar += info.Size()
				}
			}
			totalUploadedSoFar += current

			overallPercent := int(0)
			if totalSize > 0 {
				overallPercent = int(totalUploadedSoFar * 100 / totalSize)
			}

			// Edit progress message every 2 seconds or on completion
			now := time.Now()

			// Log at Info level only every 5 seconds (not on every callback)
			if now.Sub(partLastInfoLogTime) >= 5*time.Second && percent > 0 && percent < 100 {
				s.logger.Info("Upload in progress", zap.Int("part", partNum), zap.Int("percent", percent), zap.Int("overall_percent", overallPercent))
				partLastInfoLogTime = now
			}
			if now.Sub(partLastUpdateTime) >= 2*time.Second || percent == 100 {
				partLastUpdateTime = now
				partProgressMsg := fmt.Sprintf("📤 **Uploading Part %d of %d**\nFile: %s\nPart Progress: %d%%\nOverall Progress: %d%%", partNum, len(rarParts), rarFileName, percent, overallPercent)

				// Only update if message content actually changed
				if partProgressMsg == lastPartProgressMsg && partStatusMsgID > 0 {
					return
				}
				lastPartProgressMsg = partProgressMsg

				// If no status message exists yet, send one. Otherwise edit it.
				if partStatusMsgID == 0 {
					newMsgID, err := messenger.SendMessage(context.Background(), partProgressMsg)
					if err != nil {
						s.logger.Error("Failed to send upload status message", zap.Error(err))
						return
					}
					partStatusMsgID = newMsgID
					s.logger.Debug("Upload status message sent", zap.Int("msg_id", partStatusMsgID))
				} else {
					err := messenger.UpdateMessage(context.Background(), partStatusMsgID, partProgressMsg)
					if err != nil {
						s.logger.Error("Failed to update upload status message", zap.Error(err), zap.Int("msg_id", partStatusMsgID))
						// If update fails, send new message
						newMsgID, sendErr := messenger.SendMessage(context.Background(), partProgressMsg)
						if sendErr == nil {
							messenger.DeleteMessage(context.Background(), partStatusMsgID)
							partStatusMsgID = newMsgID
						}
						return
					}
					s.logger.Debug("Upload status message edited", zap.Int("msg_id", partStatusMsgID))
				}
			}
		})

		cancel()

		if err != nil {
			s.logger.Error("Failed to upload RAR part", zap.Error(err), zap.Int("number", partNum))
			// Delete upload message on error
			if partStatusMsgID > 0 {
				deleteErr := messenger.DeleteMessage(context.Background(), partStatusMsgID)
				if deleteErr != nil {
					s.logger.Error("Failed to delete upload message on error", zap.Error(deleteErr), zap.Int("msg_id", partStatusMsgID))
				} else {
					s.logger.Debug("Upload message deleted on error", zap.Int("msg_id", partStatusMsgID))
				}
			}
			errorMsg := fmt.Sprintf("❌ **Part %d Failed**\nError: %v", partNum, err)
			messenger.SendMessage(ctx, errorMsg)
			continue
		}

		uploadedParts++
		s.logger.Debug("RAR part upload complete", zap.Int("number", partNum))

		// Delete the upload message when complete (don't keep old messages)
		if partStatusMsgID > 0 {
			deleteErr := messenger.DeleteMessage(context.Background(), partStatusMsgID)
			if deleteErr != nil {
				s.logger.Error("Failed to delete upload message on success", zap.Error(deleteErr), zap.Int("msg_id", partStatusMsgID))
			} else {
				s.logger.Debug("Upload message deleted on success", zap.Int("msg_id", partStatusMsgID))
			}
		}

		// Small delay between parts
		time.Sleep(500 * time.Millisecond)
	}

	// Cleanup RAR files
	rarSplitter.CleanupRarFiles(rarParts)
	s.logger.Info("RAR files cleaned up")

	s.logger.Info("Test upload complete", zap.String("file", filePath))
}
