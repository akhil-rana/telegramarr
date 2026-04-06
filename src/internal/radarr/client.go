package radarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// MovieMetadata represents movie information from Radarr API
type MovieMetadata struct {
	TmdbID     int     `json:"tmdbId"`
	ImdbID     string  `json:"imdbId"`
	Title      string  `json:"title"`
	Overview   string  `json:"overview"`
	Year       int     `json:"year"`
	Runtime    int     `json:"runtime"`
	Popularity float64 `json:"popularity"`
	PosterURL  string  `json:"posterUrl"` // We'll extract this from Images
	Images     []struct {
		CoverType string `json:"coverType"`
		URL       string `json:"url"`
	} `json:"images"`
	MovieRatings struct {
		Tmdb struct {
			Value float64 `json:"value"`
			Count int     `json:"count"`
		} `json:"tmdb"`
		Imdb struct {
			Value float64 `json:"value"`
			Count int     `json:"count"`
		} `json:"imdb"`
	} `json:"movieRatings"`
}

// Client handles requests to Radarr API
type Client struct {
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClient creates a new Radarr API client
func NewClient(logger *zap.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// GetMovieByTMDBID fetches movie metadata from Radarr API using TMDB ID
func (c *Client) GetMovieByTMDBID(ctx context.Context, tmdbID int) (*MovieMetadata, error) {
	if tmdbID == 0 {
		return nil, fmt.Errorf("invalid TMDB ID: %d", tmdbID)
	}

	// Use public Radarr API endpoint (no authentication needed)
	url := fmt.Sprintf("https://api.radarr.video/v1/movie/%d", tmdbID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		c.logger.Error("Failed to create request", zap.Error(err), zap.String("url", url))
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("Failed to fetch movie metadata", zap.Error(err), zap.String("url", url))
		return nil, fmt.Errorf("failed to fetch movie metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Warn("Unexpected status code from Radarr API",
			zap.Int("status_code", resp.StatusCode),
			zap.String("body", string(body)),
			zap.String("url", url))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var metadata MovieMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		c.logger.Error("Failed to decode response", zap.Error(err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract poster URL from Images array
	for _, img := range metadata.Images {
		if img.CoverType == "Poster" {
			metadata.PosterURL = img.URL
			break
		}
	}

	c.logger.Info("Fetched movie metadata from Radarr API",
		zap.Int("tmdb_id", tmdbID),
		zap.String("title", metadata.Title),
		zap.String("poster_url", metadata.PosterURL))

	return &metadata, nil
}
