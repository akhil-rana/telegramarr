package config

type Config struct {
	Telegram TelegramConfig `mapstructure:"telegram" validate:"required"`
	Paths    PathsConfig    `mapstructure:"paths"`
	TMDB     TMDBConfig     `mapstructure:"tmdb"`
	Server   ServerConfig   `mapstructure:"server"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

type TelegramConfig struct {
	AppID           int    `mapstructure:"app_id" validate:"required"`
	AppHash         string `mapstructure:"app_hash" validate:"required"`
	RadarrChannelID int64  `mapstructure:"radarr_channel_id" validate:"required"`
	SonarrChannelID int64  `mapstructure:"sonarr_channel_id" validate:"required"`
	DeviceModel     string `mapstructure:"device_model" default:"Desktop"`
	SystemVersion   string `mapstructure:"system_version" default:"Windows 11"`
	AppVersion      string `mapstructure:"app_version" default:"6.7.0"`
	LangPack        string `mapstructure:"lang_pack" default:"tdesktop"`
	SystemLangCode  string `mapstructure:"system_lang_code" default:"en-US"`
	LangCode        string `mapstructure:"lang_code" default:"en"`

	// Rate limiting configuration (matching teldrive architecture)
	RateLimit  bool `mapstructure:"rate_limit" default:"false"`
	Rate       int  `mapstructure:"rate" default:"100"`
	RateBurst  int  `mapstructure:"rate_burst" default:"5"`
	MaxRetries int  `mapstructure:"max_retries" default:"5"`

	// Upload performance configuration
	UploadThreads  int `mapstructure:"upload_threads" default:"8"`
	UploadPartSize int `mapstructure:"upload_part_size" default:"512"`
	PoolSize       int `mapstructure:"pool_size" default:"8"`

	// Message refresh interval for upload progress (in seconds)
	MessageRefreshInterval int `mapstructure:"message_refresh_interval" default:"2"`

	// Delay time before processing webhook (in seconds) - allows filesystem to catch up, especially for rclone mounts
	DelayTime int `mapstructure:"delay_time" default:"30"`
}

type TMDBConfig struct {
	// Enable/disable TMDB movie details and poster fetching
	Enabled bool `mapstructure:"enabled" default:"false"`
	// API key for TMDB (required if enabled is true)
	APIKey string `mapstructure:"api_key"`
	// Base URL for TMDB images (e.g., https://image.tmdb.org/t/p/)
	ImageBaseURL string `mapstructure:"image_base_url" default:"https://image.tmdb.org/t/p/"`
	// Poster size (w92, w154, w185, w342, w500, w780, original)
	PosterSize string `mapstructure:"poster_size" default:"w342"`
	// Backdrop size (w300, w780, w1280, original)
	BackdropSize string `mapstructure:"backdrop_size" default:"w780"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port" default:"8080"`
	Host string `mapstructure:"host" default:"0.0.0.0"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level" default:"info"`
	Format string `mapstructure:"format" default:"json"`
}

type PathsConfig struct {
	// Path where Radarr stores movies (with trailing slash)
	RadarrMoviesPath string `mapstructure:"radarr_movies" default:"./movies/"`
	// Path where Sonarr stores TV shows (with trailing slash)
	SonarrTVShowsPath string `mapstructure:"sonarr_tvshows" default:"./tvshows/"`
}
