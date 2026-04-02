package uploader

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// TelegramUploader handles uploading files to Telegram
type TelegramUploader struct {
	client    *telegram.Client
	logger    *zap.Logger
	messenger *ChannelMessenger
}

// NewTelegramUploader creates a new Telegram uploader
func NewTelegramUploader(client *telegram.Client, channelID int64, logger *zap.Logger) *TelegramUploader {
	messenger := NewChannelMessenger(client, channelID, logger)
	return &TelegramUploader{
		client:    client,
		logger:    logger,
		messenger: messenger,
	}
}

// UploadToChannel uploads a file to a Telegram channel
// Uses gotd/td's uploader with multiple threads for fast upload
func (tu *TelegramUploader) UploadToChannel(ctx context.Context, filePath string, fileName string, progressCallback func(current, total int64, percent int)) (int64, error) {
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
						// Speed in MB/s
						bytesSinceLast := current - lastProgress
						speedMBps := float64(bytesSinceLast) / elapsed.Seconds() / 1024 / 1024

						// Estimate remaining time
						remainingBytes := fileSize - current
						if bytesSinceLast > 0 {
							remainingSeconds := time.Duration(float64(remainingBytes) / (float64(bytesSinceLast) / elapsed.Seconds()))
							tu.logger.Info("Upload progress",
								zap.Int64("current", current),
								zap.Int64("total", fileSize),
								zap.Int("percent", percent),
								zap.Float64("speed_mbps", speedMBps),
								zap.Duration("eta", remainingSeconds))
						}
					}

					progressCallback(current, fileSize, percent)
					lastProgress = current
					lastProgressTime = time.Now()
				}
			}
		},
	}

	// Get fresh API client for upload
	apiClient := tu.client.API()

	// Create file uploader with multiple threads (like teldrive does)
	// Use 8 threads for high speed (same as teldrive default)
	// Rate limiting and backoff is handled at the part level in the upload orchestrator
	// 512KB part size (standard Telegram upload size)
	fileUploader := uploader.NewUploader(apiClient).
		WithThreads(8).
		WithPartSize(512 * 1024)

	// Upload file to Telegram
	tu.logger.Info("Starting file upload to Telegram", zap.String("file", fileName), zap.Int64("size", fileSize))

	uploadedFile, err := fileUploader.Upload(ctx, uploader.NewUpload(fileName, progressReader, fileSize))
	if err != nil {
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

	// Send the document using message builder (like teldrive)
	sender := message.NewSender(apiClient)
	target := sender.To(&tg.InputPeerChannel{
		ChannelID:  tu.messenger.channelID,
		AccessHash: freshAccessHash,
	})

	msgResult, err := target.Media(ctx, document)
	if err != nil {
		tu.logger.Error("Failed to send document to channel", zap.Error(err))
		return 0, fmt.Errorf("failed to send document: %w", err)
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
