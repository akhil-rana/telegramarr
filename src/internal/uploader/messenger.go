package uploader

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"github.com/akhil-rana/telegramarr/internal/pool"
)

// ChannelMessenger handles sending messages to a Telegram channel
type ChannelMessenger struct {
	client      *telegram.Client
	pool        pool.Pool
	channelID   int64
	logger      *zap.Logger
	accessHash  int64
	accessMutex sync.Once
}

// NewChannelMessenger creates a new channel messenger
// When pool is provided, it will be used for concurrent message operations
// When pool is nil, the client will be used directly for backwards compatibility
func NewChannelMessenger(client *telegram.Client, channelID int64, logger *zap.Logger, poolInstance pool.Pool) *ChannelMessenger {
	return &ChannelMessenger{
		client:    client,
		pool:      poolInstance,
		channelID: channelID,
		logger:    logger,
	}
}

// SendMessage sends a text message to the channel with HTML formatting support
func (cm *ChannelMessenger) SendMessage(ctx context.Context, text string) (int, error) {
	if cm.client == nil {
		cm.logger.Error("Client is nil, cannot send message")
		return 0, fmt.Errorf("client is nil")
	}

	// Strip HTML tags and get entities
	cleanedText, entities := stripHTMLAndParseEntities(text)

	// Fetch AccessHash if not already done
	var err error
	cm.accessMutex.Do(func() {
		cm.accessHash, err = cm.fetchChannelAccessHash(ctx)
	})
	if err != nil {
		cm.logger.Error("Failed to fetch channel access hash", zap.Error(err))
		return 0, fmt.Errorf("failed to fetch channel access hash: %w", err)
	}

	cm.logger.Debug("Using channel peer", zap.Int64("channel_id", cm.channelID), zap.Int64("access_hash", cm.accessHash))

	inputPeer := &tg.InputPeerChannel{
		ChannelID:  cm.channelID,
		AccessHash: cm.accessHash,
	}

	// Get API client from pool if available
	apiClient := cm.getAPIClient(ctx)

	result, err := apiClient.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:      inputPeer,
		Message:   cleanedText,
		RandomID:  generateRandomID(),
		NoWebpage: true,
		Entities:  entities,
	})

	if err != nil {
		cm.logger.Error("Failed to send message", zap.Error(err), zap.String("text", cleanedText[:min(len(cleanedText), 50)]))
		return 0, fmt.Errorf("failed to send message: %w", err)
	}

	// Extract message ID from result
	msgID := extractMessageID(result)
	cm.logger.Debug("Message sent to channel", zap.Int("msg_id", msgID))

	if msgID == 0 {
		cm.logger.Warn("Message ID was 0, checking result type", zap.String("result_type", fmt.Sprintf("%T", result)))
		// Debug: log the actual response structure
		if r, ok := result.(*tg.Updates); ok {
			cm.logger.Debug("Updates response details", zap.Int("num_updates", len(r.Updates)), zap.Int("num_chats", len(r.Chats)), zap.Int("num_users", len(r.Users)))
			for i, update := range r.Updates {
				cm.logger.Debug("Update details", zap.Int("index", i), zap.String("type", fmt.Sprintf("%T", update)))
			}
		}
	}

	return msgID, nil
}

// UpdateMessage edits an existing message with HTML formatting support
func (cm *ChannelMessenger) UpdateMessage(ctx context.Context, msgID int, text string) error {
	// If msgID is 0, we can't edit - just log and return error
	if msgID == 0 {
		cm.logger.Warn("Cannot update message with ID 0 - message was sent but ID could not be extracted")
		return fmt.Errorf("message ID is 0 - cannot update")
	}

	if cm.client == nil {
		cm.logger.Error("Client is nil, cannot update message")
		return fmt.Errorf("client is nil")
	}

	// Strip HTML tags and get entities
	cleanedText, entities := stripHTMLAndParseEntities(text)

	// Fetch AccessHash if not already done
	var err error
	cm.accessMutex.Do(func() {
		cm.accessHash, err = cm.fetchChannelAccessHash(ctx)
	})
	if err != nil {
		cm.logger.Error("Failed to fetch channel access hash", zap.Error(err))
		return fmt.Errorf("failed to fetch channel access hash: %w", err)
	}

	inputPeer := &tg.InputPeerChannel{
		ChannelID:  cm.channelID,
		AccessHash: cm.accessHash,
	}

	// Get API client from pool if available
	apiClient := cm.getAPIClient(ctx)

	_, err = apiClient.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
		Peer:      inputPeer,
		ID:        msgID,
		Message:   cleanedText,
		Entities:  entities,
		NoWebpage: true,
	})

	if err != nil {
		cm.logger.Error("Failed to update message", zap.Error(err), zap.Int("msg_id", msgID))
		return fmt.Errorf("failed to update message: %w", err)
	}

	cm.logger.Debug("Message updated", zap.Int("msg_id", msgID))
	return nil
}

// DeleteMessage deletes a message from the channel
func (cm *ChannelMessenger) DeleteMessage(ctx context.Context, msgID int) error {
	// If msgID is 0, we can't delete
	if msgID == 0 {
		cm.logger.Warn("Cannot delete message with ID 0")
		return fmt.Errorf("message ID is 0 - cannot delete")
	}

	if cm.client == nil {
		cm.logger.Error("Client is nil, cannot delete message")
		return fmt.Errorf("client is nil")
	}

	// Fetch AccessHash if not already done
	var err error
	cm.accessMutex.Do(func() {
		cm.accessHash, err = cm.fetchChannelAccessHash(ctx)
	})
	if err != nil {
		cm.logger.Error("Failed to fetch channel access hash for deletion", zap.Error(err))
		return fmt.Errorf("failed to fetch channel access hash: %w", err)
	}

	inputChannel := &tg.InputChannel{
		ChannelID:  cm.channelID,
		AccessHash: cm.accessHash,
	}

	// Get API client from pool if available
	apiClient := cm.getAPIClient(ctx)

	// Use ChannelsDeleteMessages for channel messages
	_, err = apiClient.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
		Channel: inputChannel,
		ID:      []int{msgID},
	})

	if err != nil {
		cm.logger.Error("Failed to delete message", zap.Error(err), zap.Int("msg_id", msgID), zap.Int64("channel_id", cm.channelID))
		return fmt.Errorf("failed to delete message: %w", err)
	}

	cm.logger.Info("Message deleted from channel", zap.Int("msg_id", msgID), zap.Int64("channel_id", cm.channelID))
	return nil
}

// getAPIClient returns an API client from the pool if available, otherwise from the base client
func (cm *ChannelMessenger) getAPIClient(ctx context.Context) *tg.Client {
	if cm.pool != nil {
		return cm.pool.Default(ctx)
	}
	return cm.client.API()
}

// fetchChannelAccessHash fetches the access hash for the channel
func (cm *ChannelMessenger) fetchChannelAccessHash(ctx context.Context) (int64, error) {
	cm.logger.Info("Fetching channel access hash", zap.Int64("channel_id", cm.channelID))

	// Create InputChannel with channel ID (access hash is 0 initially, but API will work)
	channels := []tg.InputChannelClass{
		&tg.InputChannel{
			ChannelID:  cm.channelID,
			AccessHash: 0, // Initial request with 0, will still work
		},
	}

	// Get API client from pool if available
	apiClient := cm.getAPIClient(ctx)

	result, err := apiClient.ChannelsGetChannels(ctx, channels)
	if err != nil {
		cm.logger.Error("Failed to get channel info", zap.Error(err))
		return 0, fmt.Errorf("failed to get channel info: %w", err)
	}

	// Extract channel from result
	if chats := result.GetChats(); chats != nil {
		for _, chat := range chats {
			if channel, ok := chat.(*tg.Channel); ok {
				if channel.ID == cm.channelID {
					cm.logger.Info("Channel access hash fetched", zap.Int64("access_hash", channel.AccessHash))
					return channel.AccessHash, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("channel %d not found in API response", cm.channelID)
}

// extractMessageID extracts message ID from API response
// Handles multiple response types from SendMessage RPC
func extractMessageID(result tg.UpdatesClass) int {
	// Try UpdateShortSentMessage first (most common case)
	if r, ok := result.(*tg.UpdateShortSentMessage); ok {
		return r.ID
	}

	// Try Updates - search through all updates for message info
	if r, ok := result.(*tg.Updates); ok {
		if len(r.Updates) > 0 {
			// Check all updates in order
			for _, update := range r.Updates {
				// Check UpdateNewMessage (for regular chats)
				if u, ok := update.(*tg.UpdateNewMessage); ok {
					if msg, ok := u.Message.(*tg.Message); ok {
						return msg.ID
					}
				}
				// Check UpdateNewChannelMessage (for channels)
				if u, ok := update.(*tg.UpdateNewChannelMessage); ok {
					if msg, ok := u.Message.(*tg.Message); ok {
						return msg.ID
					}
				}
			}
		}
	}

	// Try UpdateShortChatMessage
	if r, ok := result.(*tg.UpdateShortChatMessage); ok {
		return r.ID
	}

	// Try UpdateShortMessage
	if r, ok := result.(*tg.UpdateShortMessage); ok {
		return r.ID
	}

	// Unknown type - return 0
	return 0
}

// generateRandomID generates a random ID for Telegram messages
func generateRandomID() int64 {
	return rand.Int63()
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// stripHTMLAndParseEntities strips HTML tags from text and returns both cleaned text and entities
// Supports: <b>bold</b>, <i>italic</i>, <u>underline</u>, <s>strike</s>, <code>code</code>
// Important: Uses UTF-16 offsets as required by Telegram API
func stripHTMLAndParseEntities(text string) (string, []tg.MessageEntityClass) {
	var entities []tg.MessageEntityClass

	type openTag struct {
		name   string
		offset int // UTF-16 offset
	}

	// Stack to track open tags
	stack := []openTag{}

	// Build cleaned text and track entities
	var cleanText strings.Builder
	utf16Offset := 0

	runes := []rune(text) // Convert to runes to properly handle multi-byte UTF-8 characters
	for i := 0; i < len(runes); i++ {
		char := runes[i]

		// Check for tag start
		if char == '<' {
			// Find closing >
			closeIdx := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '>' {
					closeIdx = j
					break
				}
			}

			if closeIdx == -1 {
				// No closing >, treat < as regular character
				cleanText.WriteRune(char)
				utf16Offset += countUTF16(string(char))
				continue
			}

			// Extract tag content
			tagContent := string(runes[i+1 : closeIdx])

			// Check if closing tag
			if strings.HasPrefix(tagContent, "/") {
				tagName := strings.TrimPrefix(tagContent, "/")
				tagName = strings.TrimSpace(tagName)

				// Find matching opening tag (last one on stack with this name)
				for j := len(stack) - 1; j >= 0; j-- {
					if stack[j].name == tagName {
						// Create entity
						startOffset := stack[j].offset
						length := utf16Offset - startOffset

						var entity tg.MessageEntityClass
						switch tagName {
						case "b":
							entity = &tg.MessageEntityBold{
								Offset: startOffset,
								Length: length,
							}
						case "i":
							entity = &tg.MessageEntityItalic{
								Offset: startOffset,
								Length: length,
							}
						case "u":
							entity = &tg.MessageEntityUnderline{
								Offset: startOffset,
								Length: length,
							}
						case "s":
							entity = &tg.MessageEntityStrike{
								Offset: startOffset,
								Length: length,
							}
						case "code":
							entity = &tg.MessageEntityCode{
								Offset: startOffset,
								Length: length,
							}
						}

						if entity != nil {
							entities = append(entities, entity)
						}

						// Remove from stack
						stack = append(stack[:j], stack[j+1:]...)
						break
					}
				}

				// Skip to after the closing tag
				i = closeIdx
				continue
			}

			// Opening tag - extract tag name (everything before first space)
			tagName := tagContent
			if spaceIdx := strings.IndexAny(tagContent, " \t"); spaceIdx >= 0 {
				tagName = tagContent[:spaceIdx]
			}

			switch tagName {
			case "b", "i", "u", "s", "code":
				stack = append(stack, openTag{name: tagName, offset: utf16Offset})
			}

			// Skip to after the closing tag
			i = closeIdx
			continue
		}

		// Regular character - add to clean text
		cleanText.WriteRune(char)
		utf16Offset += countUTF16(string(char))
	}

	return cleanText.String(), entities
}

// countUTF16 counts the number of UTF-16 code units in a string
// Most characters are 1 code unit, but surrogate pairs (emoji, etc) are 2
func countUTF16(s string) int {
	count := 0
	for _, r := range s {
		if r > 0xFFFF {
			// Surrogate pair - counts as 2 UTF-16 code units
			count += 2
		} else {
			count += 1
		}
	}
	return count
}

// utf16Len returns the UTF-16 length of a character (legacy, use countUTF16)
func utf16Len(char string) int {
	return countUTF16(char)
}

// parseHTMLEntities is a legacy wrapper that only returns entities
// Use stripHTMLAndParseEntities instead for the cleaned text
func parseHTMLEntities(text string) []tg.MessageEntityClass {
	_, entities := stripHTMLAndParseEntities(text)
	return entities
}
