# Telegramarr

Telegram file upload manager for Radarr/Sonarr with authentication.

## Quick Start

### Run Backend
```bash
./dev.sh
```

Visit: http://localhost:8080

### Frontend Development (Optional - if npm works)
In another terminal:
```bash
cd ui && npm run dev
```

Then visit: http://localhost:5173

## Build for Production

```bash
./build.sh
```

Then run:
```bash
cd src && ./telegramarr
```

## Configuration

Edit `src/config.yaml` to change channel ID and other settings.

## Features

- ✓ QR code authentication
- ✓ Phone number + SMS authentication  
- ✓ 2FA support
- ✓ Session persistence
- ✓ Beautiful web UI
- ✓ Simple logout button
- ✓ No database required

## API Endpoints

- `GET /api/auth/status` - Check auth status
- `POST /api/auth/init` - Start auth (QR code)
- `POST /api/auth/phone` - Send SMS code
- `POST /api/auth/code` - Submit code
- `POST /api/auth/2fa` - Submit 2FA password
- `POST /api/auth/logout` - Logout

