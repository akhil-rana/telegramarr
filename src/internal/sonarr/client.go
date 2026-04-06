package sonarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// SeriesMetadata represents series information from Skyhook API
type SeriesMetadata struct {
	TvdbID    int    `json:"tvdbId"`
	ImdbID    string `json:"imdbId"`
	Title     string `json:"title"`
	Overview  string `json:"overview"`
	Year      int    `json:"year"`
	Runtime   int    `json:"runtime"`
	PosterURL string `json:"posterUrl"` // We'll extract this from Images
	Status    string `json:"status"`
	Rating    struct {
		Count int     `json:"count"`
		Value float64 `json:"value,string"` // Value is returned as string in JSON
	} `json:"rating"`
	Images []struct {
		CoverType string `json:"coverType"`
		Url       string `json:"url"`
	} `json:"images"`
}

// EpisodeMetadata represents episode information from Sonarr API
type EpisodeMetadata struct {
	EpisodeNumber int    `json:"episodeNumber"`
	SeasonNumber  int    `json:"seasonNumber"`
	Title         string `json:"title"`
	Overview      string `json:"overview"`
	AirDate       string `json:"airDate"`
}

// Client handles requests to Sonarr API
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClient creates a new Sonarr API client
// baseURL: Sonarr instance URL (e.g., "http://localhost:8989")
// apiKey: Sonarr API key (optional for public endpoints)
func NewClient(baseURL string, apiKey string, logger *zap.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// GetSeriesByTvdbID fetches series metadata from Skyhook API using TVDB ID
func (c *Client) GetSeriesByTvdbID(ctx context.Context, tvdbID int) (*SeriesMetadata, error) {
	if tvdbID == 0 {
		return nil, fmt.Errorf("invalid TVDB ID: %d", tvdbID)
	}

	// Use Skyhook public metadata API (Sonarr's official metadata service)
	// Format: https://skyhook.sonarr.tv/v1/tvdb/shows/{language}/{tvdbId}
	url := fmt.Sprintf("https://skyhook.sonarr.tv/v1/tvdb/shows/en/%d", tvdbID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		c.logger.Error("Failed to create request", zap.Error(err), zap.String("url", url))
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("Failed to fetch series metadata", zap.Error(err), zap.String("url", url))
		return nil, fmt.Errorf("failed to fetch series metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Warn("Unexpected status code from Sonarr API",
			zap.Int("status_code", resp.StatusCode),
			zap.String("body", string(body)),
			zap.String("url", url))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var metadata SeriesMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		c.logger.Error("Failed to decode response", zap.Error(err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract poster URL from Images array
	for _, img := range metadata.Images {
		if img.CoverType == "Poster" {
			metadata.PosterURL = img.Url
			break
		}
	}

	c.logger.Info("Fetched series metadata from Sonarr API",
		zap.Int("tvdb_id", tvdbID),
		zap.String("title", metadata.Title),
		zap.String("poster_url", metadata.PosterURL))

	return &metadata, nil
}
