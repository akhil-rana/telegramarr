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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akhil-rana/telegramarr/internal/auth"
	"github.com/akhil-rana/telegramarr/internal/config"
	"github.com/akhil-rana/telegramarr/internal/pool"
	tgc "github.com/akhil-rana/telegramarr/internal/telegram"
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

// Webhook queue item types
type webhookQueueItem struct {
	itemType string // "radarr" or "sonarr"
	radarr   *webhook.RadarrWebhookPayload
	sonarr   *webhook.SonarrWebhookPayload
}

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
	// Webhook queue for sequential processing with delays
	webhookQueue chan webhookQueueItem
	queueDone    chan struct{}
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
		webhookQueue:  make(chan webhookQueueItem, 1000), // Queue up to 1000 webhooks
		queueDone:     make(chan struct{}),
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

	// Start webhook queue processor
	go s.webhookQueueProcessor()

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

// webhookQueueProcessor processes webhooks sequentially with delay between them
func (s *Server) webhookQueueProcessor() {
	var lastProcessTime time.Time
	delaySeconds := time.Duration(s.config.App.DelayTime) * time.Second

	for item := range s.webhookQueue {
		// Calculate delay from last processing time
		timeSinceLastProcess := time.Since(lastProcessTime)
		if timeSinceLastProcess < delaySeconds && !lastProcessTime.IsZero() {
			// Wait for remaining delay time
			waitTime := delaySeconds - timeSinceLastProcess
			s.logger.Debug("Webhook queue: Waiting before processing next item",
				zap.Duration("wait_time", waitTime),
				zap.Duration("configured_delay", delaySeconds))
			time.Sleep(waitTime)
		}

		// Process the webhook based on type
		switch item.itemType {
		case "radarr":
			s.logger.Info("Processing queued Radarr webhook")
			s.processRadarrWebhook(item.radarr)
		case "sonarr":
			s.logger.Info("Processing queued Sonarr webhook")
			s.processSonarrWebhook(item.sonarr)
		}

		// Update last process time
		lastProcessTime = time.Now()
	}

	// Signal that queue processing is done
	close(s.queueDone)
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
				s.logger.Error("Failed to read WebSocket message", zap.Error(err))
				return err
			}

			// Handle authentication based on auth_type
			switch msg.AuthType {
			case "qr":
				// Handle QR code authentication
				s.logger.Info("Processing QR code authentication")
				s.handleQRAuth(ctx, conn, tgClient, &dispatcher, sessionStorage)
				return nil

			case "phone":
				// Handle phone authentication
				s.logger.Info("Processing phone authentication")
				s.handlePhoneAuth(ctx, conn, tgClient, msg, sessionStorage)
				return nil

			case "2fa":
				// Handle 2FA password
				s.logger.Info("Processing 2FA password")
				// 2FA is handled within phone auth flow
				conn.WriteJSON(map[string]any{
					"type":    "error",
					"message": "2FA must be handled during phone authentication flow",
				})
				return nil

			default:
				conn.WriteJSON(map[string]any{
					"type":    "error",
					"message": "Unknown auth type: " + msg.AuthType,
				})
			}
		}
	})

	if err != nil {
		s.logger.Error("Telegram client error", zap.Error(err))
	}
}

// sendPosterToChannel sends poster image with metadata (runtime, genres, ratings) and synopsis, returning the message ID
func (s *Server) sendPosterToChannel(ctx context.Context, posterURL string, title string, imdbID string, year int, overview string, channelID int64, runtime int, genres []string, imdbRating float64, filename string, quality string, fileSize int64) (int, error) {
	s.logger.Info("SENDING POSTER IMAGE WITH FULL DESCRIPTION AS CAPTION",
		zap.String("title", title),
		zap.String("imdb_id", imdbID),
		zap.Int("year", year),
		zap.String("poster_url", posterURL),
		zap.Int64("channel_id", channelID),
		zap.Int("runtime", runtime),
		zap.Strings("genres", genres),
		zap.Float64("imdb_rating", imdbRating),
		zap.String("filename", filename),
		zap.String("quality", quality),
		zap.Int64("filesize", fileSize))

	if s.tgClient == nil {
		s.logger.Error("Telegram client not initialized - cannot send poster and description")
		return 0, fmt.Errorf("telegram client not initialized")
	}

	// Create connection pool (hardcoded to 8)
	poolSize := int64(8)

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

	// 1. DOWNLOAD POSTER IMAGE
	s.logger.Info("Step 1: Downloading poster image from TMDB", zap.String("poster_url", posterURL))

	resp, err := http.Get(posterURL)
	if err != nil {
		s.logger.Error("Failed to download poster image", zap.Error(err), zap.String("poster_url", posterURL))
		return 0, fmt.Errorf("failed to download poster: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("Failed to download poster image - non-200 status", zap.Int("status_code", resp.StatusCode), zap.String("poster_url", posterURL))
		return 0, fmt.Errorf("failed to download poster: status %d", resp.StatusCode)
	}

	// Read image data
	posterData, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("Failed to read poster image data", zap.Error(err))
		return 0, fmt.Errorf("failed to read poster data: %w", err)
	}

	s.logger.Info("Poster image downloaded successfully", zap.Int64("size_bytes", int64(len(posterData))))

	// 2. BUILD FULL DESCRIPTION CAPTION FOR THE IMAGE
	s.logger.Info("Step 2: Building full description caption for poster image", zap.String("title", title))

	var captionBuilder strings.Builder

	// Add title as IMDb link
	if imdbID != "" {
		captionBuilder.WriteString(fmt.Sprintf("<a href=\"https://www.imdb.com/title/%s/\">%s</a>", imdbID, title))
	} else {
		captionBuilder.WriteString(fmt.Sprintf("<b>%s</b>", title))
	}

	// Add year in italics
	if year > 0 {
		captionBuilder.WriteString(fmt.Sprintf(" <i>(%d)</i>", year))
	}

	// Add line break after title
	captionBuilder.WriteString("\n")

	// Add metadata line: Runtime | Genres | IMDb Rating
	var metadataItems []string

	if runtime > 0 {
		metadataItems = append(metadataItems, fmt.Sprintf("%d min", runtime))
	}

	if len(genres) > 0 {
		metadataItems = append(metadataItems, strings.Join(genres, ", "))
	}

	if imdbRating > 0 {
		metadataItems = append(metadataItems, fmt.Sprintf("⭐ %.1f IMDb", imdbRating))
	}

	if len(metadataItems) > 0 {
		captionBuilder.WriteString(strings.Join(metadataItems, " | "))
	}

	// Add full overview as caption
	if overview != "" {
		captionBuilder.WriteString("\n\n")
		captionBuilder.WriteString(overview)
	}

	// Add file metadata at the end in blockquote
	captionBuilder.WriteString("\n\n<blockquote>")
	if filename != "" {
		captionBuilder.WriteString(fmt.Sprintf("<b>File Name:</b> <code>%s</code>\n", filename))
	}
	if quality != "" {
		captionBuilder.WriteString(fmt.Sprintf("<b>Quality:</b> %s\n", quality))
	}
	if fileSize > 0 {
		captionBuilder.WriteString(fmt.Sprintf("<b>Size:</b> %s", formatBytes(fileSize)))
	}
	captionBuilder.WriteString("</blockquote>")

	caption := captionBuilder.String()

	s.logger.Info("Caption prepared for poster image",
		zap.String("title", title),
		zap.String("caption_preview", caption[:min(len(caption), 100)]))

	// 3. SEND POSTER IMAGE WITH FULL DESCRIPTION AS CAPTION (FIRST MESSAGE)
	s.logger.Info("Step 3: Sending poster image with full description caption as FIRST MESSAGE", zap.String("title", title))

	msgID, err := messenger.SendPhotoFromBytes(ctx, posterData, caption)
	if err != nil {
		s.logger.Error("Failed to send poster image to Telegram", zap.Error(err), zap.String("title", title))
		return 0, fmt.Errorf("failed to send poster image: %w", err)
	}

	s.logger.Info("✅ COMPLETE: Poster image with description sent successfully as FIRST MESSAGE",
		zap.String("title", title),
		zap.Int("msg_id", msgID),
		zap.String("next", "File upload will follow as reply"))

	return msgID, nil
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

	// Create and initialize Telegram client from the authenticated session
	// This allows immediate use of the client without requiring a server restart
	client, err := auth.CreateClientFromSession(ctx, s.config.Telegram.AppID, s.config.Telegram.AppHash, sessionDataObj, &s.config.Telegram, s.logger)
	if err != nil {
		s.logger.Error("QR auth: Failed to create client from session", zap.Error(err))
		conn.WriteJSON(map[string]any{"type": "error", "message": "failed to initialize client"})
		return
	}

	s.tgClient = client

	// Create ClientManager to run client in background
	// This maintains the connection and keeps the session alive
	s.clientManager = auth.NewClientManager(client, s.logger)

	s.logger.Info("QR auth: Client initialized and manager started")

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

		// Create and initialize Telegram client from the authenticated session
		// This allows immediate use of the client without requiring a server restart
		client, err := auth.CreateClientFromSession(ctx, s.config.Telegram.AppID, s.config.Telegram.AppHash, sessionDataObj, &s.config.Telegram, s.logger)
		if err != nil {
			s.logger.Error("Phone auth: Failed to create client from session", zap.Error(err))
			conn.WriteJSON(map[string]any{"type": "error", "message": "failed to initialize client"})
			return
		}

		s.tgClient = client

		// Create ClientManager to run client in background
		// This maintains the connection and keeps the session alive
		s.clientManager = auth.NewClientManager(client, s.logger)

		s.logger.Info("Phone auth: Client initialized and manager started")

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

	// Create and initialize Telegram client from the authenticated session
	// This allows immediate use of the client without requiring a server restart
	client, err := auth.CreateClientFromSession(ctx, s.config.Telegram.AppID, s.config.Telegram.AppHash, sessionDataObj, &s.config.Telegram, s.logger)
	if err != nil {
		s.logger.Error("2FA auth: Failed to create client from session", zap.Error(err))
		conn.WriteJSON(map[string]any{"type": "error", "message": "failed to initialize client"})
		return
	}

	s.tgClient = client

	// Create ClientManager to run client in background
	// This maintains the connection and keeps the session alive
	s.clientManager = auth.NewClientManager(client, s.logger)

	s.logger.Info("2FA auth: Client initialized and manager started")

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
	s.logger.Info("Logout request received", zap.Bool("has_client", s.tgClient != nil), zap.Bool("has_session", s.session != nil))

	// Call auth.logOut on Telegram servers to invalidate session remotely
	if s.tgClient != nil && s.session != nil && s.session.Authenticated {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		s.logger.Info("Attempting to sign out from Telegram servers via auth.logOut RPC")

		// Run the client to make the logout RPC call
		err := s.tgClient.Run(logoutCtx, func(ctx context.Context) error {
			// Call auth.logOut to invalidate session on Telegram servers
			logOutReq := &tg.AuthLogOutRequest{}
			logOutResp := &tg.BoolTrue{} // Simple response type that implements bin.Decoder

			if err := s.tgClient.Invoke(ctx, logOutReq, logOutResp); err != nil {
				s.logger.Warn("Failed to call auth.logOut on Telegram servers", zap.Error(err))
				// Continue with local cleanup even if remote logout fails
				return nil
			}
			s.logger.Info("Successfully signed out from Telegram servers")
			return nil
		})

		if err != nil && err != context.DeadlineExceeded {
			s.logger.Warn("Error during Telegram logout RPC call", zap.Error(err))
		}
	}

	// Stop client manager - this closes the active connection
	if s.clientManager != nil {
		s.logger.Info("Stopping client manager to close Telegram connection")
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.clientManager.Stop(stopCtx); err != nil {
			s.logger.Warn("Error stopping client manager", zap.Error(err))
		}
		s.clientManager = nil
	}

	// Delete session files - remove device authorization from local storage
	s.logger.Info("Deleting session files", zap.String("session_path", s.sessionPath))
	if err := auth.DeleteSession(s.sessionPath, s.sessionDBPath); err != nil {
		s.logger.Error("Failed to delete session files", zap.Error(err))
		c.JSON(500, gin.H{"error": "failed to logout"})
		return
	}

	s.logger.Info("Session files deleted successfully")

	// Clear client and session from memory
	s.tgClient = nil
	s.session = nil

	s.logger.Info("Device successfully logged out from Telegram and local session deleted")

	c.JSON(200, gin.H{
		"status":  "logged_out",
		"message": "Successfully logged out - device removed from Telegram servers and local session terminated",
	})
}

func (s *Server) GetSettings(c *gin.Context) {
	if s.session == nil || !s.session.Authenticated {
		c.JSON(401, gin.H{"error": "not authenticated"})
		return
	}

	c.JSON(200, gin.H{
		"app_id":         s.config.Telegram.AppID,
		"radarr_channel": s.config.App.RadarrChannelID,
		"sonarr_channel": s.config.App.SonarrChannelID,
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

	// Queue webhook for sequential processing
	s.webhookQueue <- webhookQueueItem{
		itemType: "radarr",
		radarr:   &payload,
	}

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

	// Queue webhook for sequential processing
	s.webhookQueue <- webhookQueueItem{
		itemType: "sonarr",
		sonarr:   &payload,
	}

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

// formatSpeed calculates and formats speed in MB/s using binary units (1024*1024)
// This shows actual transfer speed, independent of file size display units
func formatSpeed(bytes int64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return "0.0 MB/s"
	}
	mbs := float64(bytes) / (1024 * 1024) / elapsed.Seconds()
	return fmt.Sprintf("%.1f MB/s", mbs)
}

// formatSpeedDelta calculates and formats speed based on bytes in a time delta (for current speed)
// Uses binary units (1024*1024) to show actual transfer speed
func formatSpeedDelta(bytes int64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return "0.0 MB/s"
	}
	mbs := float64(bytes) / (1024 * 1024) / elapsed.Seconds()
	return fmt.Sprintf("%.1f MB/s", mbs)
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

	return fmt.Sprintf(
		"<b>📦 Splitting file into archives</b>\n"+
			"\n"+
			"<b>Title:</b> %s\n"+
			"<b>File:</b> <b><code>%s</code></b>\n"+
			"\n"+
			"<b>Splitting into archives...</b>\n"+
			"%s %d%%\n"+
			"\n"+
			"<b>Progress:</b> %s / %s",
		title, fileName, bar, percent, currentMB, totalMB,
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

	// If only one file, don't show "Part 1 of 1"
	var header string
	if totalFiles == 1 {
		header = "📤 <b>Uploading</b>\n"
	} else {
		header = fmt.Sprintf("📤 <b>Uploading Part %d of %d</b>\n", fileNum, totalFiles)
	}

	return header +
		"\n" +
		fmt.Sprintf("<b>Source:</b> %s\n", strings.ToUpper(source)) +
		fmt.Sprintf("<b>Title:</b> %s\n", title) +
		fmt.Sprintf("<b>File:</b> <b><code>%s</code></b>\n", fileName) +
		"\n" +
		fmt.Sprintf("%s %d%%\n", bar, percent) +
		"\n" +
		fmt.Sprintf("<b>Progress:</b> %s / %s\n", currentMB, totalMB) +
		fmt.Sprintf("<b>Speed:</b> %s\n", speed) +
		eta
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
	delayTime := time.Duration(s.config.App.DelayTime) * time.Second
	s.logger.Info("Radarr webhook: waiting before processing",
		zap.Duration("delay", delayTime))
	time.Sleep(delayTime)

	// Map Radarr path to container path if needed
	filePath := payload.MovieFile.Path
	if strings.HasPrefix(filePath, "/movies/") && s.config.Paths.RadarrMoviesPath != "/movies/" {
		// Replace /movies/ with configured path
		filePath = strings.Replace(filePath, "/movies/", s.config.Paths.RadarrMoviesPath, 1)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); err != nil {
		s.logger.Error("Radarr file does not exist", zap.Error(err), zap.String("originalPath", payload.MovieFile.Path), zap.String("mappedPath", filePath))
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

	// Fetch and send poster image and description if available (only if configured)
	posterMsgID := 0
	if s.config.App.SendMovieDetailsMessage {
		metadata, err := s.fetchRadarrMetadata(ctx, payload.Movie.TmdbID)
		if err == nil && metadata != nil {
			// Find poster URL
			posterURL := ""
			for _, img := range metadata.Images {
				if img.CoverType == "Poster" {
					posterURL = img.URL
					break
				}
			}

			if posterURL != "" {
				s.logger.Info("Sending Radarr poster and description to channel", zap.String("poster_url", posterURL))
				// Send poster and description SYNCHRONOUSLY (wait for it to complete before uploading file)
				filename := filepath.Base(filePath)
				// Get actual file size from disk instead of using webhook payload
				fileInfo, err := os.Stat(filePath)
				var actualFileSize int64 = 0
				if err == nil {
					actualFileSize = fileInfo.Size()
				}
				msgID, err := s.sendPosterToChannel(ctx, posterURL, metadata.Title, metadata.ImdbID, metadata.Year, metadata.Overview, s.config.App.RadarrChannelID, metadata.Runtime, metadata.Genres, metadata.MovieRatings.Imdb.Value, filename, payload.MovieFile.Quality, actualFileSize)
				if err != nil {
					s.logger.Error("Failed to send poster and description", zap.Error(err))
					posterMsgID = 0
				} else {
					posterMsgID = msgID
				}
			}
		} else {
			s.logger.Debug("Could not fetch Radarr metadata", zap.Error(err))
		}
	} else {
		s.logger.Debug("Radarr movie details message disabled in config")
	}

	// Process file upload with metadata for filename shortening
	s.uploadWebhookFile(ctx, filePath, payload.Movie.Title, payload.Movie.ImdbID, payload.Movie.TmdbID, "radarr", s.config.App.RadarrChannelID, &uploader.ShortenerConfig{
		MovieTitle:   payload.Movie.Title,
		Quality:      payload.MovieFile.Quality,
		Format:       payload.MovieFile.MediaInfo.VideoCodec,
		ReleaseGroup: payload.MovieFile.ReleaseGroup,
	}, payload.MovieFile.Quality, payload.MovieFile.Size, payload.Movie.Overview, payload.Movie.Year, posterMsgID)
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
	delayTime := time.Duration(s.config.App.DelayTime) * time.Second
	s.logger.Info("Sonarr webhook: waiting before processing",
		zap.Duration("delay", delayTime))
	time.Sleep(delayTime)

	// Map Sonarr path to container path if needed
	filePath := payload.EpisodeFile.Path
	if strings.HasPrefix(filePath, "/tvshows/") && s.config.Paths.SonarrTVShowsPath != "/tvshows/" {
		// Replace /tvshows/ with configured path
		filePath = strings.Replace(filePath, "/tvshows/", s.config.Paths.SonarrTVShowsPath, 1)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); err != nil {
		s.logger.Error("Sonarr file does not exist", zap.Error(err), zap.String("originalPath", payload.EpisodeFile.Path), zap.String("mappedPath", filePath))
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

	// Fetch and send poster image and description if available (only if configured)
	posterMsgID := 0
	if s.config.App.SendSeriesDetailsMessage {
		metadata, err := s.fetchSonarrMetadata(ctx, payload.Series.TvdbID)
		if err == nil && metadata != nil {
			// Find poster URL
			posterURL := ""
			for _, img := range metadata.Images {
				if img.CoverType == "Poster" {
					posterURL = img.URL
					break
				}
			}

			if posterURL != "" {
				s.logger.Info("Sending Sonarr poster and description to channel", zap.String("poster_url", posterURL))
				// Send poster and description SYNCHRONOUSLY (wait for it to complete before uploading file)
				// Get the first episode overview if available
				episodeOverview := ""
				if len(payload.Episodes) > 0 {
					episodeOverview = payload.Episodes[0].Overview
				}

				filename := filepath.Base(filePath)
				// Get actual file size from disk instead of using webhook payload
				fileInfo, err := os.Stat(filePath)
				var actualFileSize int64 = 0
				if err == nil {
					actualFileSize = fileInfo.Size()
				}
				msgID, err := s.sendPosterToChannel(ctx, posterURL, metadata.Title, metadata.ImdbID, metadata.Year, episodeOverview, s.config.App.SonarrChannelID, metadata.Runtime, metadata.Genres, metadata.Rating.Value, filename, payload.EpisodeFile.Quality, actualFileSize)
				if err != nil {
					s.logger.Error("Failed to send poster and description", zap.Error(err))
					posterMsgID = 0
				} else {
					posterMsgID = msgID
				}
			}
		} else {
			s.logger.Debug("Could not fetch Sonarr metadata", zap.Error(err))
		}
	} else {
		s.logger.Debug("Sonarr series details message disabled in config")
	}

	// Process file upload
	episodeOverview := ""
	if len(payload.Episodes) > 0 {
		episodeOverview = payload.Episodes[0].Overview
	}
	s.uploadWebhookFile(ctx, filePath, payload.Series.Title, payload.Series.ImdbID, payload.Series.TvdbID, "sonarr", s.config.App.SonarrChannelID, nil, payload.EpisodeFile.Quality, payload.EpisodeFile.Size, episodeOverview, payload.Series.Year, posterMsgID)
}

// uploadWebhookFile handles the common file upload logic for both Radarr and Sonarr webhooks
// metadata: optional ShortenerConfig for RAR filename shortening (only used for RAR splits)
// overview: optional overview/description text from webhook payload
// year: optional year for display
func (s *Server) uploadWebhookFile(ctx context.Context, filePath string, title string, imdbIDOrTvdbString interface{}, tmdbIDOrTvdbInt interface{}, source string, channelID int64, metadata *uploader.ShortenerConfig, quality string, fileSize int64, overview string, year int, posterMessageID int) {
	// Check file exists and get actual file size from disk
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		s.logger.Error("File does not exist", zap.Error(err), zap.String("path", filePath), zap.String("source", source))
		return
	}

	// Always use actual file size from disk, ignore webhook fileSize parameter
	fileSize = fileInfo.Size()
	s.logger.Info("Using actual file size from disk", zap.String("file", filePath), zap.Int64("size", fileSize))
	isPremium := s.session != nil && s.session.Premium

	// Extract IMDB ID for IMDb link
	imdbID := ""
	if id, ok := imdbIDOrTvdbString.(string); ok && id != "" {
		imdbID = id
	}

	// Determine size limit based on archive_split_size config
	// Telegram uses 1024 for KB→MB, then 1000 for MB→GB conversion
	// So exact sizes are: 1GB = 1,048,576,000 bytes, 2GB = 2,097,152,000 bytes, 4GB = 4,194,304,000 bytes
	var sizeLimit int64
	var maxAllowedSizeGB float64

	// Determine max allowed based on account type
	if isPremium {
		maxAllowedSizeGB = 4.0 // 4GB for premium
	} else {
		maxAllowedSizeGB = 2.0 // 2GB for free
	}

	if s.config.App.ArchiveSplitSize > 0 {
		// Use custom split size from config, but enforce account type limit
		configSizeGB := s.config.App.ArchiveSplitSize
		if configSizeGB > maxAllowedSizeGB {
			s.logger.Warn("Configured archive split size exceeds account limit, clamping to max allowed",
				zap.Float64("configured_size_gb", configSizeGB),
				zap.Float64("max_allowed_gb", maxAllowedSizeGB),
				zap.Bool("premium", isPremium))
			configSizeGB = maxAllowedSizeGB
		}
		// Convert GB to bytes using Telegram's formula: GB * 1024 * 1024 * 1000
		sizeLimit = int64(configSizeGB * 1024 * 1024 * 1000)
		s.logger.Info("Using custom archive split size from config", zap.Float64("split_size_gb", configSizeGB), zap.Int64("size_limit_bytes", sizeLimit))
	} else {
		// Use maximum according to account type
		if isPremium {
			sizeLimit = int64(4194304000) // 4GB exact
		} else {
			sizeLimit = int64(2097152000) // 2GB exact
		}
		s.logger.Info("Using default archive split size based on account type", zap.Bool("premium", isPremium), zap.Int64("size_limit_bytes", sizeLimit))
	}

	// If file is smaller than limit, upload directly without RAR
	if fileSize < sizeLimit {
		s.uploadWebhookFileDirectly(ctx, filePath, title, source, fileSize, quality, imdbID, channelID, overview, year, posterMessageID)
		return
	}

	// File is larger than limit, use RAR splitting
	s.uploadWebhookFileWithRar(ctx, filePath, title, source, isPremium, sizeLimit, channelID, metadata, posterMessageID)
}

// uploadWebhookFileDirectly uploads a file without RAR splitting
// overview: optional overview/description text from webhook payload
// year: optional year for display
// posterMessageID: if > 0, file upload will be sent as a reply to this message
func (s *Server) uploadWebhookFileDirectly(ctx context.Context, filePath string, title string, source string, fileSize int64, quality string, imdbID string, channelID int64, overview string, year int, posterMessageID int) {
	s.logger.Info("Uploading file directly (no RAR needed)", zap.String("file", filePath), zap.Int64("size", fileSize), zap.String("source", source))

	isPremium := s.session != nil && s.session.Premium

	// Create caption for the file with just filename
	fileName := filepath.Base(filePath)

	// Build caption with just filename in monospace
	var captionBuilder strings.Builder
	captionBuilder.WriteString(fmt.Sprintf("<code>%s</code>", fileName))

	caption := captionBuilder.String()

	s.logger.Info("Generated caption for file", zap.String("caption", caption), zap.String("file", filePath))

	// Create captions map to pass to uploader
	filesCaptions := map[string]string{
		filePath: caption,
	}

	// Use centralized upload mechanism with caption
	if err := s.uploadFilesToChannel(ctx, []string{filePath}, title, source, isPremium, channelID, filesCaptions, 0, posterMessageID); err != nil {
		s.logger.Error("File upload failed", zap.Error(err), zap.String("source", source))
	}

	s.logger.Info("Webhook file upload complete", zap.String("source", source), zap.String("title", title), zap.String("file", fileName))
}

// uploadWebhookFileWithRar uploads a large file using archive splitting (RAR or 7z) via centralized upload mechanism
// metadata: optional ShortenerConfig for archive filename shortening
// posterMessageID: if > 0, archive parts will be sent as replies to this message
func (s *Server) uploadWebhookFileWithRar(ctx context.Context, filePath string, title string, source string, isPremium bool, sizeLimit int64, channelID int64, metadata *uploader.ShortenerConfig, posterMessageID int) {
	// Create messenger for archive progress updates
	// Connection pool size (hardcoded to 8)
	var poolSize int64 = 8
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
		"<b>📦 Splitting file into archives</b>\n\n"+
			"<b>Title:</b> %s\n"+
			"<b>File:</b> %s\n\n"+
			"<b>Preparing Archive Parts...</b>",
		title, fileName,
	)

	s.logger.Info("Sending initial webhook message to channel", zap.String("title", title), zap.String("source", source))
	msgID, err := messenger.SendMessage(ctx, startMsg)
	if err != nil {
		s.logger.Error("Failed to send initial webhook message", zap.Error(err), zap.String("source", source))
		return
	}

	// Split file with progress callback using configured archive format
	archiver := uploader.NewArchiver(s.config.App.SplitArchiveFormat, s.logger)

	// Create temp directory at project root using the config helper
	tempArchiveDir, err := config.GetTempDir()
	if err != nil {
		s.logger.Error("Failed to get temp directory", zap.Error(err))
		updateMsg := fmt.Sprintf("❌ <b>Error getting temp directory:</b> %v", err)
		messenger.UpdateMessage(ctx, msgID, updateMsg)
		return
	}

	s.logger.Info("Starting archive split for webhook", zap.String("file", filePath), zap.String("source", source), zap.String("format", s.config.App.SplitArchiveFormat))

	// Track last update time for progress messages (update interval from config)
	refreshInterval := time.Duration(s.config.App.MessageRefreshInterval) * time.Second
	s.logger.Info("Message refresh interval set", zap.Duration("interval", refreshInterval), zap.Int("config_value", s.config.App.MessageRefreshInterval))
	lastUpdateTime := time.Now()
	lastInfoLogTime := time.Now()
	var lastArchiveProgressMsg string

	// Split file with progress callback for Telegram updates
	archiveParts, err := archiver.SplitFile(filePath, tempArchiveDir, 0, isPremium, s.config.App.ArchiveSplitSize, func(bytesProcessed, totalBytes int64, percent int) {
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
			progressMsg := formatArchiveProgressMessage(fileName, source, title, s.config.App.SplitArchiveFormat, percent, bytesProcessed, totalBytes)

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

	// Create captions for archive parts (filename only)
	archivePartsCaptions := make(map[string]string)
	for _, partPath := range archiveParts {
		partName := filepath.Base(partPath)
		archivePartsCaptions[partPath] = fmt.Sprintf("<code>%s</code>", partName)
	}

	// Use centralized upload mechanism for archive parts, passing the existing message ID to reuse it
	if err := s.uploadFilesToChannel(ctx, archiveParts, title, source, isPremium, channelID, archivePartsCaptions, msgID, posterMessageID); err != nil {
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
// posterMessageID: if provided (>0), files will be sent as replies to this message
func (s *Server) uploadFilesToChannel(ctx context.Context, filePaths []string, title string, source string, isPremium bool, channelID int64, filesCaptions map[string]string, existingMsgID int, posterMessageID int) error {
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

	// Connection pool size (hardcoded to 8)
	var poolSize int64 = 8

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
		if i == 0 && existingMsgID > 0 {
			// Reuse the existing message from RAR progress
			msgID = existingMsgID
		} else {
			// Create a new status message for this file
			var statusMsg string
			if totalFiles == 1 {
				statusMsg = fmt.Sprintf(
					"📤 <b>Uploading</b>\n\n"+
						"<b>Source:</b> %s\n"+
						"<b>Title:</b> %s\n"+
						"<b>File:</b> <b><code>%s</code></b>\n\n"+
						"<b>Starting...</b>",
					strings.ToUpper(source), title, fileName,
				)
			} else {
				statusMsg = fmt.Sprintf(
					"📤 <b>Uploading Part %d of %d</b>\n\n"+
						"<b>Source:</b> %s\n"+
						"<b>Title:</b> %s\n"+
						"<b>File:</b> <b><code>%s</code></b>\n\n"+
						"<b>Starting...</b>",
					fileNum, totalFiles, strings.ToUpper(source), title, fileName,
				)
			}
			msgID, _ = messenger.SendMessage(ctx, statusMsg)
		}

		// Track last update time for progress messages (update interval configurable from config)
		refreshInterval := time.Duration(s.config.App.MessageRefreshInterval) * time.Second
		lastUpdateTime := time.Now()
		uploadStartTime := time.Now()
		lastInfoLogTime := time.Now() // Track Info-level logging (every 5 seconds)
		var lastProgressMsg string

		// Track speed calculation (every 5 seconds)
		lastSpeedCheckTime := uploadStartTime
		lastSpeedCheckBytes := int64(0)
		var lastCalculatedSpeed string

		// Get caption for this file if provided
		fileCaption := ""
		if filesCaptions != nil {
			fileCaption = filesCaptions[filePath]
		}

		// Upload the file with throttled progress updates and speed calculation
		_, err := telegramUploader.UploadToChannelWithReply(ctx, filePath, fileName, fileCaption, func(current, total int64, percent int) {
			elapsed := time.Since(uploadStartTime)
			now := time.Now()

			// Calculate speed every 3 seconds only
			timeSinceLastCheck := now.Sub(lastSpeedCheckTime).Seconds()
			if timeSinceLastCheck >= 3.0 || percent == 100 {
				// Calculate speed from last checkpoint
				bytesSinceLastCheck := current - lastSpeedCheckBytes
				if timeSinceLastCheck > 0 {
					lastCalculatedSpeed = formatSpeedDelta(bytesSinceLastCheck, time.Duration(int64(timeSinceLastCheck*1000))*time.Millisecond)
				} else {
					lastCalculatedSpeed = "0.0 MB/s"
				}
				lastSpeedCheckTime = now
				lastSpeedCheckBytes = current
			}

			// Always use the last calculated speed (or "Calculating..." if not ready yet)
			currentSpeed := lastCalculatedSpeed
			if currentSpeed == "" {
				currentSpeed = "Calculating..."
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
		}, posterMessageID)

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

// RadarrMovieMetadata represents the minimal metadata we need from Radarr's public API
type RadarrMovieMetadata struct {
	Title    string   `json:"Title"`
	Year     int      `json:"Year"`
	Overview string   `json:"Overview"`
	ImdbID   string   `json:"ImdbId"`
	TmdbID   int      `json:"TmdbId"`
	Runtime  int      `json:"Runtime"`
	Genres   []string `json:"Genres"`
	Images   []struct {
		CoverType string `json:"CoverType"`
		URL       string `json:"Url"`
	} `json:"Images"`
	MovieRatings struct {
		Imdb struct {
			Value float64 `json:"Value"`
			Count int     `json:"Count"`
		} `json:"Imdb"`
	} `json:"MovieRatings"`
}

// SonarrSeriesMetadata represents the minimal metadata we need from Sonarr's Skyhook API
type SonarrSeriesMetadata struct {
	Title   string   `json:"title"`
	Year    int      `json:"year"`
	ImdbID  string   `json:"imdbId"`
	TvdbID  int      `json:"tvdbId"`
	Runtime int      `json:"runtime"`
	Genres  []string `json:"genres"`
	Images  []struct {
		CoverType string `json:"coverType"`
		URL       string `json:"url"`
	} `json:"images"`
	Rating struct {
		Value float64 `json:"value"`
	} `json:"rating"`
}

// fetchRadarrMetadata fetches movie metadata from Radarr's public API
func (s *Server) fetchRadarrMetadata(ctx context.Context, tmdbID int) (*RadarrMovieMetadata, error) {
	if tmdbID == 0 {
		s.logger.Warn("Radarr metadata fetch: invalid TMDB ID provided")
		return nil, fmt.Errorf("invalid TMDB ID")
	}

	url := fmt.Sprintf("https://api.radarr.video/v1/movie/%d", tmdbID)
	s.logger.Info("Radarr metadata fetch: calling api.radarr.video API", zap.String("url", url), zap.Int("tmdb_id", tmdbID))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		s.logger.Error("Radarr metadata fetch: failed to create API request", zap.Error(err), zap.String("url", url))
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.logger.Error("Radarr metadata fetch: failed to fetch metadata from api.radarr.video", zap.Error(err), zap.String("url", url))
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("Radarr metadata fetch: unexpected status from api.radarr.video", zap.Int("status_code", resp.StatusCode), zap.String("url", url))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	s.logger.Info("Radarr metadata fetch: api.radarr.video responded with 200 OK, parsing response")

	var metadata RadarrMovieMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		s.logger.Error("Radarr metadata fetch: failed to decode api.radarr.video response", zap.Error(err))
		return nil, err
	}

	s.logger.Info("Radarr metadata fetch: successfully decoded metadata", zap.Int("images_count", len(metadata.Images)), zap.String("title", metadata.Title), zap.Int("runtime", metadata.Runtime), zap.Strings("genres", metadata.Genres))
	return &metadata, nil
}

// fetchRadarrPosterURL fetches the poster image URL from Radarr's public API (deprecated - use fetchRadarrMetadata instead)
func (s *Server) fetchRadarrPosterURL(ctx context.Context, tmdbID int) (string, error) {
	metadata, err := s.fetchRadarrMetadata(ctx, tmdbID)
	if err != nil {
		return "", err
	}

	if metadata == nil {
		return "", fmt.Errorf("metadata is nil")
	}

	// Extract poster URL from Images array
	for i, img := range metadata.Images {
		s.logger.Debug("Radarr poster fetch: examining image", zap.Int("index", i), zap.String("cover_type", img.CoverType), zap.String("url", img.URL))
		if img.CoverType == "Poster" {
			s.logger.Info("Radarr poster fetch: FOUND poster image", zap.String("poster_url", img.URL))
			return img.URL, nil
		}
	}

	s.logger.Warn("Radarr poster fetch: no poster image found in api.radarr.video response")
	return "", fmt.Errorf("no poster image found")
}

// fetchSonarrMetadata fetches series metadata from Sonarr's Skyhook API
func (s *Server) fetchSonarrMetadata(ctx context.Context, tvdbID int) (*SonarrSeriesMetadata, error) {
	if tvdbID == 0 {
		s.logger.Warn("Sonarr metadata fetch: invalid TVDB ID provided")
		return nil, fmt.Errorf("invalid TVDB ID")
	}

	url := fmt.Sprintf("https://skyhook.sonarr.tv/v1/tvdb/shows/en/%d", tvdbID)
	s.logger.Info("Sonarr metadata fetch: calling skyhook.sonarr.tv API", zap.String("url", url), zap.Int("tvdb_id", tvdbID))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		s.logger.Error("Sonarr metadata fetch: failed to create API request", zap.Error(err), zap.String("url", url))
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.logger.Error("Sonarr metadata fetch: failed to fetch metadata from skyhook.sonarr.tv", zap.Error(err), zap.String("url", url))
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("Sonarr metadata fetch: unexpected status from skyhook.sonarr.tv", zap.Int("status_code", resp.StatusCode), zap.String("url", url))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	s.logger.Info("Sonarr metadata fetch: skyhook.sonarr.tv responded with 200 OK, parsing response")

	var metadata SonarrSeriesMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		s.logger.Error("Sonarr metadata fetch: failed to decode skyhook.sonarr.tv response", zap.Error(err))
		return nil, err
	}

	s.logger.Info("Sonarr metadata fetch: successfully decoded metadata", zap.Int("images_count", len(metadata.Images)), zap.String("title", metadata.Title), zap.Int("runtime", metadata.Runtime), zap.Strings("genres", metadata.Genres))
	return &metadata, nil
}

// fetchSonarrPosterURL fetches the poster image URL from Sonarr's Skyhook API (deprecated - use fetchSonarrMetadata instead)
func (s *Server) fetchSonarrPosterURL(ctx context.Context, tvdbID int) (string, error) {
	metadata, err := s.fetchSonarrMetadata(ctx, tvdbID)
	if err != nil {
		return "", err
	}

	if metadata == nil {
		return "", fmt.Errorf("metadata is nil")
	}

	// Extract poster URL from Images array
	for i, img := range metadata.Images {
		s.logger.Debug("Sonarr poster fetch: examining image", zap.Int("index", i), zap.String("cover_type", img.CoverType), zap.String("url", img.URL))
		if img.CoverType == "Poster" {
			s.logger.Info("Sonarr poster fetch: FOUND poster image", zap.String("poster_url", img.URL))
			return img.URL, nil
		}
	}

	s.logger.Warn("Sonarr poster fetch: no poster image found in skyhook.sonarr.tv response")
	return "", fmt.Errorf("no poster image found")
}
