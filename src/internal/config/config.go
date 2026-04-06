package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func Load(configPath string, logger *zap.Logger) (*Config, error) {
	if configPath == "" {
		configPath = "config.yaml"
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s\nPlease create config.yaml with:\ntelegram:\n  app_id: YOUR_ID\n  app_hash: \"YOUR_HASH\"\n  channel_id: YOUR_CHANNEL_ID", configPath)
	}

	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Environment variable overrides
	viper.SetEnvPrefix("TELEGRAMARR")
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")
	viper.SetDefault("telegram.message_refresh_interval", 2)
	viper.SetDefault("telegram.delay_time", 30)
	viper.SetDefault("paths.radarr_movies", "./movies/")
	viper.SetDefault("paths.sonarr_tvshows", "./tvshows/")
	viper.SetDefault("tmdb.enabled", false)
	viper.SetDefault("telegram.archive_split_size", 0)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields
	if cfg.Telegram.AppID == 0 {
		return nil, fmt.Errorf("telegram.app_id is required")
	}
	if cfg.Telegram.AppHash == "" {
		return nil, fmt.Errorf("telegram.app_hash is required")
	}
	if cfg.Telegram.RadarrChannelID == 0 {
		return nil, fmt.Errorf("telegram.radarr_channel_id is required")
	}
	if cfg.Telegram.SonarrChannelID == 0 {
		return nil, fmt.Errorf("telegram.sonarr_channel_id is required")
	}

	logger.Info("Config loaded",
		zap.Int("app_id", cfg.Telegram.AppID),
		zap.Int64("radarr_channel_id", cfg.Telegram.RadarrChannelID),
		zap.Int64("sonarr_channel_id", cfg.Telegram.SonarrChannelID),
		zap.Int("server_port", cfg.Server.Port),
		zap.Int("message_refresh_interval", cfg.Telegram.MessageRefreshInterval),
		zap.Int("delay_time", cfg.Telegram.DelayTime),
		zap.String("radarr_movies_path", cfg.Paths.RadarrMoviesPath),
		zap.String("sonarr_tvshows_path", cfg.Paths.SonarrTVShowsPath),
		zap.Bool("tmdb_enabled", cfg.TMDB.Enabled),
	)

	return &cfg, nil
}

// GetDataDir returns the data directory path, creating it if needed
func GetDataDir() (string, error) {
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}
	return dataDir, nil
}

// GetTempDir returns the temp directory path, creating it if needed
func GetTempDir() (string, error) {
	tempDir := "temp"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	return tempDir, nil
}

// SessionFilePath returns the path to session.json
func SessionFilePath() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "session.json"), nil
}

// SessionDBPath returns the path to session.db
func SessionDBPath() (string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "session.db"), nil
}
