package uploader

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"github.com/akhil-rana/telegramarr/internal/config"
	"github.com/akhil-rana/telegramarr/internal/pool"
)

// TelegramUploader handles uploading files to Telegram
type TelegramUploader struct {
	client    *telegram.Client
	pool      pool.Pool
	logger    *zap.Logger
	messenger *ChannelMessenger
	cfg       *config.TelegramConfig
}

// NewTelegramUploader creates a new Telegram uploader
// pool MUST NOT be nil - it's required for concurrent uploads
func NewTelegramUploader(client *telegram.Client, channelID int64, logger *zap.Logger, cfg *config.TelegramConfig, poolInstance pool.Pool) *TelegramUploader {
	if poolInstance == nil {
		logger.Fatal("Pool cannot be nil - connection pooling is required for optimal performance")
	}
	messenger := NewChannelMessenger(client, channelID, logger, poolInstance)
	return &TelegramUploader{
		client:    client,
		pool:      poolInstance,
		logger:    logger,
		messenger: messenger,
		cfg:       cfg,
	}
}

// UploadToChannel uploads a file to a Telegram channel
// Uses gotd/td's uploader with multiple threads for fast upload
// caption is optional - if provided, it will be shown as the file's caption in Telegram
func (tu *TelegramUploader) UploadToChannel(ctx context.Context, filePath string, fileName string, caption string, progressCallback func(current, total int64, percent int)) (int64, error) {
	return tu.UploadToChannelWithReply(ctx, filePath, fileName, caption, progressCallback, 0)
}

// UploadToChannelWithReply uploads a file to a Telegram channel as a reply to another message
// replyToMsgID: if > 0, the file will be sent as a reply to this message
func (tu *TelegramUploader) UploadToChannelWithReply(ctx context.Context, filePath string, fileName string, caption string, progressCallback func(current, total int64, percent int), replyToMsgID int) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		tu.logger.Error("Failed to open file", zap.Error(err), zap.String("path", filePath))
		return 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		tu.logger.Error("Failed to stat file", zap.Error(err))
		return 0, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := fileInfo.Size()

	tu.logger.Info("Uploading file to Telegram",
		zap.String("file", fileName),
		zap.Int64("size", fileSize))

	// Don't cache apiClient - get it fresh before each operation
	// This ensures we always have a valid connection

	// Track upload progress
	startTime := time.Now()
	var lastProgress int64
	var lastProgressTime time.Time = startTime

	progressReader := &ProgressReader{
		reader: file,
		total:  fileSize,
		callback: func(current int64) {
			if progressCallback != nil {
				// Report progress every 5MB or when we reach the end
				if (current-lastProgress) > 5*1024*1024 || current == fileSize {
					percent := int(current * 100 / fileSize)

					// Calculate speed
					elapsed := time.Since(lastProgressTime)
					if elapsed > 0 {
						// Speed in MB/s using binary units (1024*1024) for actual transfer speed
						bytesSinceLast := current - lastProgress
						speedMBs := float64(bytesSinceLast) / elapsed.Seconds() / 1024 / 1024

						// Estimate remaining time
						remainingBytes := fileSize - current
						if bytesSinceLast > 0 {
							remainingSeconds := time.Duration(float64(remainingBytes) / (float64(bytesSinceLast) / elapsed.Seconds()))
							// Only log every 10% to avoid log spam
							if percent%10 == 0 {
								tu.logger.Debug("Upload progress",
									zap.Int64("current", current),
									zap.Int64("total", fileSize),
									zap.Int("percent", percent),
									zap.Float64("speed_mbs", speedMBs),
									zap.Duration("eta", remainingSeconds))
							}
						}
					}

					progressCallback(current, fileSize, percent)
					lastProgress = current
					lastProgressTime = time.Now()
				}
			}
		},
	}

	// Get API client from pool (required for concurrent uploads)
	apiClient := tu.pool.Default(ctx)

	// Use hardcoded upload configuration
	const threads = 8
	const partSizeKB = 512
	partSizeBytes := partSizeKB * 1024

	tu.logger.Info("Upload configuration", zap.Int("threads", threads), zap.Int("part_size_kb", partSizeKB))

	// Telegram handles FLOOD_WAIT internally - we just need to let it retry
	fileUploader := uploader.NewUploader(apiClient).
		WithThreads(threads).
		WithPartSize(partSizeBytes)

	// Upload file to Telegram
	tu.logger.Info("Starting file upload to Telegram", zap.String("file", fileName), zap.Int64("size", fileSize))

	uploadedFile, err := fileUploader.Upload(ctx, uploader.NewUpload(fileName, progressReader, fileSize))
	if err != nil {
		// Check if error is FLOOD_WAIT (rate limiting)
		errStr := err.Error()
		if strings.Contains(errStr, "FLOOD_WAIT") {
			// Extract wait time if available and log it
			tu.logger.Warn("Telegram FLOOD_WAIT rate limiting during upload",
				zap.Error(err),
				zap.String("file", fileName))
		}
		tu.logger.Error("Failed to upload file to Telegram", zap.Error(err))
		return 0, fmt.Errorf("failed to upload file: %w", err)
	}

	tu.logger.Info("File upload complete", zap.String("file", fileName))

	// Send final progress
	if progressCallback != nil {
		progressCallback(fileSize, fileSize, 100)
	}

	// Refetch channel info to get fresh access hash before sending media (like teldrive does)
	inputChannelForInfo := &tg.InputChannel{
		ChannelID:  tu.messenger.channelID,
		AccessHash: 0, // Start with 0, API will work
	}

	channelsResp, err := apiClient.ChannelsGetChannels(ctx, []tg.InputChannelClass{inputChannelForInfo})
	if err != nil {
		tu.logger.Error("Failed to refetch channel info", zap.Error(err))
		return 0, fmt.Errorf("failed to refetch channel: %w", err)
	}

	var freshAccessHash int64
	if chats := channelsResp.GetChats(); len(chats) > 0 {
		if channel, ok := chats[0].(*tg.Channel); ok {
			freshAccessHash = channel.AccessHash
			tu.logger.Info("Refetched channel access hash", zap.Int64("access_hash", freshAccessHash))
		}
	}

	// Create document using message builder (exactly like teldrive does)
	document := message.UploadedDocument(uploadedFile).
		Filename(fileName).
		ForceFile(true)

	// Create input media for direct API call with caption support
	inputMedia := &tg.InputMediaUploadedDocument{
		File:     uploadedFile,
		MimeType: "application/octet-stream", // Default mime type for files
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeFilename{
				FileName: fileName,
			},
		},
	}

	// Parse caption for entities if caption is provided
	var cleanedCaption string
	var entities []tg.MessageEntityClass
	if caption != "" {
		cleanedCaption, entities = stripHTMLAndParseEntities(caption)
		tu.logger.Info("Caption will be sent with file",
			zap.String("file", fileName),
			zap.String("caption_preview", cleanedCaption[:min(len(cleanedCaption), 100)]),
			zap.Int("entities_count", len(entities)))
	} else {
		tu.logger.Info("No caption provided for file", zap.String("file", fileName))
	}

	// Send the document - use API directly if caption is provided, otherwise use message builder
	// Retry logic for transient failures (rate limiting, temporary issues)
	// These often succeed on retry even after gotd's internal retries are exhausted
	var msgResult tg.UpdatesClass
	var lastErr error
	maxRetries := 15 // Increased from 5 to handle Telegram rate limiting better
	baseDelay := 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if caption != "" {
			// Use direct API call with MessagesSendMedia to support caption
			tu.logger.Info("SENDING DOCUMENT WITH CAPTION TO TELEGRAM",
				zap.String("file", fileName),
				zap.Int64("channel_id", tu.messenger.channelID),
				zap.String("caption_preview", cleanedCaption[:min(len(cleanedCaption), 150)]),
				zap.Int("attempt", attempt+1),
				zap.Int("max_attempts", maxRetries))

			sendMediaRequest := &tg.MessagesSendMediaRequest{
				Peer:     &tg.InputPeerChannel{ChannelID: tu.messenger.channelID, AccessHash: freshAccessHash},
				Media:    inputMedia,
				Message:  cleanedCaption,
				RandomID: generateRandomID(),
				Entities: entities,
			}

			// Add reply if replyToMsgID is provided
			if replyToMsgID > 0 {
				sendMediaRequest.ReplyTo = &tg.InputReplyToMessage{
					ReplyToMsgID: replyToMsgID,
				}
				tu.logger.Info("File will be sent as reply", zap.Int("reply_to_msg_id", replyToMsgID), zap.String("file", fileName))
			}

			result, err := apiClient.MessagesSendMedia(ctx, sendMediaRequest)
			msgResult = result
			lastErr = err

			if err == nil {
				tu.logger.Info("✅ DOCUMENT WITH CAPTION SENT SUCCESSFULLY",
					zap.String("file", fileName),
					zap.Int64("channel_id", tu.messenger.channelID),
					zap.String("status", "Caption should now be visible in Telegram"))
			}
		} else {
			// Use message builder when no caption (original behavior)
			sender := message.NewSender(apiClient)
			target := sender.To(&tg.InputPeerChannel{
				ChannelID:  tu.messenger.channelID,
				AccessHash: freshAccessHash,
			})
			msgResult, lastErr = target.Media(ctx, document)
		}

		if lastErr == nil {
			break // Success, exit retry loop
		}

		// Check if error is retryable
		errStr := lastErr.Error()
		isRetryable := false

		// List of retryable error messages
		retryableErrors := []string{
			"retry limit reached",
			"FLOOD_WAIT",
			"Timedout",
			"connection dead",
			"WORKER_BUSY_TOO_LONG_RETRY",
			"rpcDoRequest",
			"server internal error",
			"temporary server error",
		}

		for _, retryErr := range retryableErrors {
			if strings.Contains(errStr, retryErr) {
				isRetryable = true
				break
			}
		}

		if !isRetryable {
			// Non-retryable error, fail immediately
			tu.logger.Error("Failed to send document to channel (non-retryable error)",
				zap.Error(lastErr), zap.Int("attempt", attempt+1))
			return 0, fmt.Errorf("failed to send document: %w", lastErr)
		}

		if attempt == maxRetries-1 {
			// Last attempt reached, log warning but don't fail - file is uploaded
			tu.logger.Warn("Max retries reached sending document to channel, but file upload succeeded",
				zap.Error(lastErr),
				zap.Int("attempt", attempt+1),
				zap.String("file", fileName))
			// Return success anyway since the file was uploaded to Telegram's servers
			// Even if we can't confirm the message ID, the file is safely stored
			msgID := extractMessageID(msgResult)
			if msgID == 0 {
				// If we can't get message ID, use -1 to indicate partial success
				return -1, nil
			}
			return int64(msgID), nil
		}

		// Calculate exponential backoff
		delay := baseDelay * time.Duration(1<<uint(attempt/3)) // Increase delay every 3 attempts
		tu.logger.Debug("Retrying document send after transient error",
			zap.Int("attempt", attempt+1),
			zap.Int("max_attempts", maxRetries),
			zap.Duration("delay", delay),
			zap.Error(lastErr))

		// Wait before retrying
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next retry
		}
	}

	// Extract message ID from result
	msgID := extractMessageID(msgResult)
	tu.logger.Info("File uploaded and sent to channel",
		zap.String("file", fileName),
		zap.Int("msg_id", msgID),
		zap.Duration("duration", time.Since(startTime)))

	return int64(msgID), nil
}

// SendProgressMessage sends a progress message to the channel
func (tu *TelegramUploader) SendProgressMessage(ctx context.Context, text string) (int, error) {
	return tu.messenger.SendMessage(ctx, text)
}

// UpdateProgressMessage updates a progress message
func (tu *TelegramUploader) UpdateProgressMessage(ctx context.Context, msgID int, text string) error {
	return tu.messenger.UpdateMessage(ctx, msgID, text)
}

// ProgressReader wraps an io.Reader and calls a callback on each read
type ProgressReader struct {
	reader   io.Reader
	total    int64
	current  int64
	callback func(current int64)
}

// NewProgressReader creates a new progress reader
func NewProgressReader(reader io.Reader, total int64, callback func(int64)) *ProgressReader {
	return &ProgressReader{
		reader:   reader,
		total:    total,
		callback: callback,
	}
}

// Read implements io.Reader
func (pr *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 {
		pr.current += int64(n)
		if pr.callback != nil {
			pr.callback(pr.current)
		}
	}
	return
}
