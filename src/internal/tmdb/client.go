package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/akhil-rana/telegramarr/internal/config"
)

const (
	baseURL = "https://api.themoviedb.org/3"
)

// Client is a TMDB API client
type Client struct {
	apiKey       string
	imageURL     string
	posterSize   string
	backdropSize string
	httpClient   *http.Client
	logger       *zap.Logger
}

// MovieDetails represents the details of a movie from TMDB
type MovieDetails struct {
	ID                  int     `json:"id"`
	Title               string  `json:"title"`
	OriginalTitle       string  `json:"original_title"`
	Overview            string  `json:"overview"`
	PosterPath          string  `json:"poster_path"`
	BackdropPath        string  `json:"backdrop_path"`
	ReleaseDate         string  `json:"release_date"`
	VoteAverage         float64 `json:"vote_average"`
	VoteCount           int     `json:"vote_count"`
	Runtime             int     `json:"runtime"`
	Genres              []Genre `json:"genres"`
	Budget              int64   `json:"budget"`
	Revenue             int64   `json:"revenue"`
	Status              string  `json:"status"`
	Tagline             string  `json:"tagline"`
	ProductionCountries []struct {
		Iso31661 string `json:"iso_3166_1"`
		Name     string `json:"name"`
	} `json:"production_countries"`
	// External IDs from TMDB (includes IMDb ID)
	ExternalIDs *ExternalIDs `json:"external_ids,omitempty"`
}

// ExternalIDs contains external identifiers for a movie
type ExternalIDs struct {
	IMDbID string `json:"imdb_id"`
}

// Genre represents a movie genre
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// NewClient creates a new TMDB client
func NewClient(cfg *config.TMDBConfig, logger *zap.Logger) *Client {
	return &Client{
		apiKey:       cfg.APIKey,
		imageURL:     cfg.ImageBaseURL,
		posterSize:   cfg.PosterSize,
		backdropSize: cfg.BackdropSize,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// GetMovieDetails fetches movie details from TMDB API
func (c *Client) GetMovieDetails(ctx context.Context, movieID int) (*MovieDetails, error) {
	url := fmt.Sprintf("%s/movie/%d?api_key=%s&append_to_response=external_ids", baseURL, movieID, c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.logger.Error("Failed to create request", zap.Error(err))
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("Failed to fetch movie details", zap.Error(err), zap.Int("movie_id", movieID))
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Error("TMDB API error", zap.Int("status", resp.StatusCode), zap.String("body", string(body)))
		return nil, fmt.Errorf("TMDB API returned status %d", resp.StatusCode)
	}

	var movie MovieDetails
	if err := json.NewDecoder(resp.Body).Decode(&movie); err != nil {
		c.logger.Error("Failed to decode movie details", zap.Error(err))
		return nil, err
	}

	c.logger.Info("Fetched movie details", zap.String("title", movie.Title), zap.Int("id", movie.ID))
	return &movie, nil
}

// GetPosterURL returns the full URL for a poster image
func (c *Client) GetPosterURL(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return c.imageURL + c.posterSize + posterPath
}

// GetBackdropURL returns the full URL for a backdrop image
func (c *Client) GetBackdropURL(backdropPath string) string {
	if backdropPath == "" {
		return ""
	}
	return c.imageURL + c.backdropSize + backdropPath
}

// DownloadImage downloads an image from the given URL and returns the bytes
func (c *Client) DownloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("image URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		c.logger.Error("Failed to create image request", zap.Error(err))
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("Failed to download image", zap.Error(err), zap.String("url", imageURL))
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Error("Image download failed", zap.Int("status", resp.StatusCode), zap.String("url", imageURL))
		return nil, fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	imageBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Error("Failed to read image data", zap.Error(err))
		return nil, err
	}

	c.logger.Info("Downloaded image", zap.String("url", imageURL), zap.Int("size_bytes", len(imageBytes)))
	return imageBytes, nil
}
