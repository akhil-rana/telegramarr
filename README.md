# Telegramarr

Telegramarr is an automated tool designed to work with Radarr and Sonarr. It listens for webhooks and sends downloaded movie and TV show files to your Telegram channel when they are successfully added to your library.

## Features

- **Automatic File Uploads**: Sends media files to Telegram when downloaded by Radarr/Sonarr
- **Large File Support**: Splits files into archives (RAR on x86-64, 7z on x86-64 and ARM64) to bypass Telegram's 2GB file size limit (4GB for premium accounts)
- **Media Details**: Optionally sends posters and metadata (title, year, IMDb/TMDb links) before uploading
- **Progress Tracking**: Real-time upload progress messages
- **Fast Uploads for Premium**: Premium Telegram accounts can disable rate limiting for faster uploads
- **Flexible Configuration**: Customize archive format, split sizes, and upload intervals
- **Web Authentication**: Browser-based QR code authentication
- **Multi-Platform**: Runs on x86-64 and ARM64 via Docker

## Installation

### Prerequisites

- Docker (recommended) or Go 1.25+ for building from source
- A Telegram account
- Radarr and/or Sonarr instances with webhook support
- Network access between Telegramarr and your Radarr/Sonarr instances

### Quick Start with Docker

1. **Create a configuration folder**:
   ```bash
   mkdir telegramarr-config
   cd telegramarr-config
   ```

2. **Copy the example configuration**:
   ```bash
   curl -o config.yaml https://raw.githubusercontent.com/akhil-rana/telegramarr/main/example.config.yaml
   ```

3. **Run Telegramarr**:
   ```bash
   docker run -d \
     --name=telegramarr \
     -p 8987:8987 \
     -v "$(pwd)/data:/app/data" \
     -v "$(pwd)/config.yaml:/app/config.yaml:ro" \
     -v "/path/to/movies:/movies:ro,shared" \
     -v "/path/to/tvshows:/tvshows:ro,shared" \
     -e TZ=UTC \
     akhilrana/telegramarr:latest
   ```

   **Note**: Files must be mounted to `/movies` and `/tvshows` inside the container.

4. **Authenticate with Telegram**:
   - Open `http://localhost:8987` in your browser
   - Click "Login with Telegram" and scan the QR code with your Telegram app
   - Complete the authentication flow (phone verification, 2FA if enabled)
   - Once authenticated, Telegramarr is ready for webhooks

5. **Verify it's running**:
   ```bash
   curl http://localhost:8987/health
   ```

## Configuration

All settings are in `config.yaml`. Here's what you need to configure:

### Getting Telegram Channel IDs

You need to specify which Telegram channel receives your files:

1. **Get Your Channel ID**: Use [@getidsbot](https://t.me/getidsbot) on Telegram
   - Forward any message from your target channel to the bot
   - Bot replies with the channel ID
   - Remove the leading `-100` if present (e.g., `-100123456789` → `123456789`)

2. **Important**: Currently only channels are supported (not private chats or groups)

### Key Settings

```yaml
app:
  radarr_channel_id: YOUR_CHANNEL_ID        # Where to send Radarr movies
  sonarr_channel_id: YOUR_CHANNEL_ID        # Where to send Sonarr episodes
  
  split_archive_format: "rar"               # "rar" (x86-64 only) or "7z" (all platforms)
  archive_split_size: 0                     # 0 = default (4GB premium, 2GB free)
                                            # Or: 1.5, 2, 3, 4 (in GB)
  
  message_refresh_interval: 5               # Update progress every N seconds
  delay_time: 30                            # Wait N seconds after webhook (for sync)
  
  send_movie_details_message: true          # Send poster + details before upload
  send_series_details_message: false        # Send poster + details before upload

server:
  port: 8987                                # Web server port
  host: 0.0.0.0                             # Listen on all interfaces
```

For all configuration options, see `example.config.yaml`.

## Setting Up Webhooks

Once authenticated, configure your Radarr and Sonarr instances:

### Radarr Webhook Setup

1. Go to **Settings > Connect**
2. Click **+ Add New** and select **Webhook**
3. Configure:
   - **Name**: Telegramarr
   - **URL**: `http://<telegramarr-ip>:8987/api/webhooks/radarr`
   - **Method**: POST
   - **On Import**: ✓ (checked)
   - **On Upgrade**: ✓ (checked)
4. Click **Save**

### Sonarr Webhook Setup

1. Go to **Settings > Connect**
2. Click **+ Add New** and select **Webhook**
3. Configure:
   - **Name**: Telegramarr
   - **URL**: `http://<telegramarr-ip>:8987/api/webhooks/sonarr`
   - **Method**: POST
   - **On Import**: ✓ (checked)
   - **On Upgrade**: ✓ (checked)
4. Click **Save**

## How It Works

When Radarr or Sonarr completes a download:

1. Webhook is sent to Telegramarr
2. Telegramarr waits `delay_time` seconds (for file system sync)
3. File is split into archives if needed (RAR for x86-64, 7z for both x86-64 and ARM64)
4. File chunks are uploaded to your configured Telegram channel
5. Upload completes and you receive your files in Telegram

Files larger than `archive_split_size` are automatically split into multiple archives.

## Troubleshooting

**Authentication not working?**
- Make sure you're using your personal Telegram account
- Clear browser cache and try again
- Check browser console for errors

**Files not uploading?**
- Verify `radarr_channel_id` and `sonarr_channel_id` are set correctly
- Check webhook URLs are accessible from Radarr/Sonarr
- View logs: `docker logs telegramarr`

**"Unsupported platform" errors on ARM64?**
- ARM64 users must use `split_archive_format: "7z"` (RAR not supported on ARM64)

**Session expired?**
- Re-authenticate via web UI at `http://localhost:8987`

**Rate limiting / slow uploads?**
- Premium Telegram account: Set `rate_limit: false` in config
- Free account: Keep `rate_limit: true`

## Docker Compose Example

```yaml
version: '3.8'

services:
  telegramarr:
    image: akhilrana/telegramarr:latest
    container_name: telegramarr
    ports:
      - "8987:8987"
    volumes:
      - ./data:/app/data
      - ./config.yaml:/app/config.yaml:ro
      - /path/to/movies:/movies:ro,shared
      - /path/to/tvshows:/tvshows:ro,shared
    environment:
      - TZ=UTC
    restart: unless-stopped
```

Save as `docker-compose.yml` and run:
```bash
docker-compose up -d
```

## Notes

- **File Paths**: Files must be at `/movies` and `/tvshows` inside the container
  - **Docker**: Mount your media directories to `/movies` and `/tvshows`
  - **Development**: Using `./dev.sh`, create `movies/` and `tvshows/` folders in the project root
- **Session Storage**: Your session is stored in `data/session.json` - keep this private
- **Docker Volumes**: Use `:shared` propagation for media folders
- **Network**: Radarr/Sonarr must be able to reach Telegramarr's webhook endpoint

## License

See [LICENSE](https://github.com/akhil-rana/telegramarr/blob/main/LICENSE) file in repository
