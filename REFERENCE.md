# Telegramarr Golang Rewrite - Implementation Reference

## Project Overview

Convert Python bot-based Telegramarr to Golang with user account authentication (no bots).

**Key Requirements:**
- User account login (App ID + App Hash from my.telegram.org)
- QR code scanning OR phone number entry with 2FA support
- Web UI for authentication and settings
- Single account per deployment (no multi-user)
- NO database - store session in local files
- Radarr/Sonarr webhooks unchanged
- File compression (7z) and upload same as Python version

---

## Configuration

### config.yaml (User Creates)
```yaml
telegram:
  app_id: 123456
  app_hash: "your_app_hash_here"
  channel_id: -1001234567890  # Where files upload
```

**That's it. 3 fields.**

---

## File Storage (No Database!)

```
telegramarr/
├── config.yaml                  # User creates
├── data/
│   ├── session.json             # Session metadata (auto-created)
│   └── session.db               # TDLib binary session (auto-created)
└── temp/                        # 7z temporary files
```

### session.json (Auto-Created After Auth)
```json
{
  "user_id": 123456789,
  "phone_number": "+1234567890",
  "first_name": "John",
  "username": "johndoe",
  "authenticated": true,
  "session_created": "2026-04-02T10:30:00Z"
}
```

---

## Single Account Enforcement

- Only ONE account can be authenticated at a time
- Attempting to login with different account → must logout first
- No user selection UI
- Settings show: "Logged in as @username"

---

## Architecture

```
┌─────────────────────────────────────────────┐
│         Telegramarr (Golang)                │
├─────────────────────────────────────────────┤
│                                             │
│  Web UI (React/Vite)                        │
│  ├─ Auth page (QR code + phone)             │
│  └─ Settings (account info, logout)         │
│                                             │
│  HTTP Server (Gin)                          │
│  ├─ GET /api/auth/status                    │
│  ├─ POST /api/auth/init (QR code)           │
│  ├─ POST /api/auth/phone                    │
│  ├─ POST /api/auth/code                     │
│  ├─ POST /api/auth/2fa                      │
│  ├─ POST /api/webhooks/radarr               │
│  ├─ POST /api/webhooks/sonarr               │
│  └─ GET /api/settings                       │
│                                             │
│  Auth Module                                │
│  ├─ QR code generation (skip2/go-qrcode)    │
│  ├─ Phone auth flow                         │
│  ├─ 2FA support                             │
│  └─ Session file management                 │
│                                             │
│  Telegram Module (gotd/td)                  │
│  ├─ Telegram client wrapper                 │
│  ├─ File upload                             │
│  └─ Session persistence                     │
│                                             │
│  Webhook Handlers                           │
│  ├─ Radarr webhook handler                  │
│  ├─ Sonarr webhook handler                  │
│  ├─ Schema validation                       │
│  └─ Job queue (goroutines)                  │
│                                             │
│  Compression Module                         │
│  ├─ Call 7z CLI                             │
│  ├─ Split files >2GB                        │
│  └─ Cleanup temp files                      │
│                                             │
└─────────────────────────────────────────────┘
```

---

## Project Structure

```
telegramarr/
├── cmd/
│   └── telegramarr/
│       └── main.go                 # Entry point
├── internal/
│   ├── api/
│   │   ├── handlers.go             # HTTP endpoints
│   │   └── middleware.go           # Auth middleware
│   ├── auth/
│   │   ├── session.go              # Load/save session.json
│   │   ├── telegram.go             # QR + phone + 2FA
│   │   ├── qr.go                   # QR code generation
│   │   └── types.go                # Session struct
│   ├── telegram/
│   │   ├── client.go               # Telegram wrapper
│   │   ├── upload.go               # File upload logic
│   │   └── types.go                # Models
│   ├── radarr/
│   │   ├── webhook.go              # Radarr handler
│   │   └── types.go                # Schema
│   ├── sonarr/
│   │   ├── webhook.go              # Sonarr handler
│   │   └── types.go                # Schema
│   ├── compression/
│   │   └── archive.go              # 7z wrapper
│   ├── config/
│   │   ├── config.go               # Load config.yaml
│   │   └── types.go                # Config struct
│   ├── logging/
│   │   └── logger.go               # Structured logging
│   └── utils/
│       ├── validation.go           # JSON schema validation
│       └── helpers.go              # Utilities
├── pkg/
│   └── models/
│       ├── radarr.go               # Radarr payload
│       └── sonarr.go               # Sonarr payload
├── ui/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── pages/
│   │   │   ├── Auth.tsx            # QR + phone login
│   │   │   └── Settings.tsx        # Account info
│   │   └── components/
│   │       ├── QRScanner.tsx
│   │       └── PhoneInput.tsx
│   ├── vite.config.ts
│   └── package.json
├── go.mod
├── Dockerfile
└── README.md
```

---

## Tech Stack

**Backend:**
- Golang 1.21+
- Gin (HTTP framework)
- gotd/td (Telegram client)
- skip2/go-qrcode (QR code)
- go-playground/validator (validation)
- Zap (logging)

**Frontend:**
- React 18
- Vite
- TypeScript
- Axios (HTTP client)
- Zustand (state management)

**Tools:**
- 7z CLI (compression)
- Docker (deployment)

---

## Authentication Flow

### QR Code
```
1. User clicks "Scan QR Code"
2. Backend creates auth request with gotd/td
3. Generate QR code image
4. Display in Web UI
5. User scans with Telegram mobile
6. Telegram server confirms
7. gotd/td receives session
8. Save session.db (TDLib binary)
9. Save session.json (metadata)
10. Return JWT token
11. Done!
```

### Phone Number
```
1. User enters phone number
2. Send auth request to Telegram
3. Telegram sends SMS/app code
4. User enters confirmation code
5. [Optional] If 2FA enabled, user enters password
6. Telegram confirms
7. gotd/td receives session
8. Save session.db + session.json
9. Return JWT token
10. Done!
```

---

## Webhook Processing

```
Webhook from Radarr/Sonarr
    ↓
Validate schema
    ↓
Extract file path
    ↓
Add to job queue (channel)
    ↓
Return 202 Accepted immediately
    ↓
Background goroutine:
    ├─ Wait delay_time (config or hardcoded)
    ├─ Check file size
    ├─ If > 2GB: compress with 7z
    ├─ Upload to Telegram
    ├─ Log result
    └─ Cleanup temp files
```

---

## Environment Variables (Optional)

```bash
# Override config file
TELEGRAMARR_TELEGRAM_APP_ID=123456
TELEGRAMARR_TELEGRAM_APP_HASH=abcdef...
TELEGRAMARR_TELEGRAM_CHANNEL_ID=-100...

# Server
TELEGRAMARR_SERVER_PORT=8080
TELEGRAMARR_SERVER_HOST=0.0.0.0

# Logging
TELEGRAMARR_LOG_LEVEL=info
TELEGRAMARR_LOG_FORMAT=json
```

---

## API Endpoints

### Authentication
```
GET  /api/auth/status              # Check if authenticated
POST /api/auth/init                # Get QR code
POST /api/auth/phone               # Send phone number
POST /api/auth/code                # Submit confirmation code
POST /api/auth/2fa                 # Submit 2FA password
POST /api/auth/logout              # Logout and delete session
```

### Settings
```
GET  /api/settings                 # Get account info
```

### Webhooks
```
POST /api/webhooks/radarr          # Radarr events
POST /api/webhooks/sonarr          # Sonarr events
```

### Health
```
GET  /                             # Health check
GET  /api/health                   # Health check
```

---

## Deployment

### Docker
```bash
# Create config
mkdir -p telegramarr-data
cat > telegramarr-data/config.yaml << EOF
telegram:
  app_id: 123456
  app_hash: "abc..."
  channel_id: -100...
EOF

# Run
docker run -p 8080:8080 \
  -v $(pwd)/telegramarr-data/config.yaml:/app/config.yaml \
  -v $(pwd)/telegramarr-data:/app/data \
  -v $(pwd)/telegramarr-data/temp:/app/temp \
  -v /path/to/movies:/app/movies:ro \
  -v /path/to/tvshows:/app/tvshows:ro \
  telegramarr:latest
```

### Radarr Webhook
- URL: `http://your-ip:8080/api/webhooks/radarr`
- Events: "On Import", "On Upgrade"

### Sonarr Webhook
- URL: `http://your-ip:8080/api/webhooks/sonarr`
- Events: "On Import", "On Upgrade"

---

## Startup Flow

```
1. Read config.yaml
   ├─ app_id: required
   ├─ app_hash: required
   └─ channel_id: required

2. Check session files
   ├─ session.json exists?
   │  ├─ YES → Load session
   │  └─ NO → Need authentication
   └─ session.db exists?
      ├─ YES → Use it
      └─ NO → Will be created

3. Start Web Server
   ├─ http://0.0.0.0:8080
   └─ Serve React frontend

4. If authenticated
   ├─ Initialize Telegram client
   ├─ Load session from session.db
   └─ Ready for webhooks ✓

5. If not authenticated
   ├─ Show auth page
   ├─ Wait for user to scan QR / enter phone
   └─ Create session files on success
```

---

## Key Implementation Notes

### Session Management (No Database)
- Load session on startup from `session.json`
- Keep in memory during runtime
- Persist TDLib session in `session.db`
- On logout: delete both files
- On restart: reload from files

### Single Account
- Check `session.json` user_id on startup
- If already authenticated with different user → error
- Enforce logout before switching accounts

### Webhook Authentication
- No auth required (same as Python)
- Just validate JSON schema
- Add to queue

### File Upload
- Use gotd/td's file upload API
- Stream large files (no loading entirely to memory)
- Send progress updates via WebSocket (optional)

### Session Persistence
- TDLib session binary stored in `session.db`
- gotd/td handles all TDLib session logic
- Just save/load the file

---

## Timeline

- **Week 1:** Setup + Config loading + basic server
- **Week 2:** Auth flow (QR + phone + 2FA)
- **Week 2-3:** Frontend (React auth UI)
- **Week 3:** Webhooks + queue processing
- **Week 4:** File upload + compression
- **Week 5:** Testing + Docker

**Total: 4-5 weeks**

---

## Notes for Self

- **gotd/td** is used by teldrive in production - proven reliable
- **No database needed** - session files are enough for single account
- **QR code auth** is more modern and user-friendly
- **Phone auth fallback** for users without Telegram mobile app
- **2FA** is Telegram feature, handled by API
- **File paths** come from Radarr/Sonarr webhooks - no config needed
- **7z compression** same as Python - call CLI
- **Single account** keeps code simple and focused
- **Local file storage** means easy debugging and no external deps
- **Embedded React UI** means single binary deployment
- **Gin framework** is lightweight and perfect for this use case

---

## Reference: Python Version Comparison

| Aspect | Python | Golang |
|--------|--------|--------|
| Config fields | 9 | 3 |
| Bot creation | Required | Not needed |
| Database | None | None (files) |
| Session | In-memory | File-based |
| Web UI | No | Yes (React) |
| Auth method | Bot token | QR + phone + 2FA |
| Multiple accounts | Limited | Single (enforced) |
| Deployment | FastAPI + script | Single binary |

---

## Quick Checklist

- [ ] Read this document
- [ ] Understand: 3-field config.yaml
- [ ] Understand: No database (files only)
- [ ] Understand: Single account only
- [ ] Understand: Web UI auth flow
- [ ] Check: Go 1.21+ installed
- [ ] Check: Node 18+ installed
- [ ] Check: 7z CLI available
- [ ] Start: Phase 1 (basic scaffolding)

Done!
