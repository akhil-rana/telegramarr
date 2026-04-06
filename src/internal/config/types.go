package config

type Config struct {
	Telegram TelegramConfig `mapstructure:"telegram" validate:"required"`
	App      AppConfig      `mapstructure:"app"`
	Server   ServerConfig   `mapstructure:"server"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

// TelegramConfig contains Telegram API authentication settings only
type TelegramConfig struct {
	AppID          int    `mapstructure:"app_id" validate:"required"`
	AppHash        string `mapstructure:"app_hash" validate:"required"`
	DeviceModel    string `mapstructure:"device_model" default:"Desktop"`
	SystemVersion  string `mapstructure:"system_version" default:"Windows 11"`
	AppVersion     string `mapstructure:"app_version" default:"6.7.1"`
	LangPack       string `mapstructure:"lang_pack" default:"tdesktop"`
	SystemLangCode string `mapstructure:"system_lang_code" default:"en-US"`
	LangCode       string `mapstructure:"lang_code" default:"en"`

	// Rate limiting configuration (matching teldrive architecture)
	RateLimit  bool `mapstructure:"rate_limit" default:"false"`
	Rate       int  `mapstructure:"rate" default:"100"`
	RateBurst  int  `mapstructure:"rate_burst" default:"5"`
	MaxRetries int  `mapstructure:"max_retries" default:"5"`
}

// AppConfig contains application settings for uploads and messaging
type AppConfig struct {
	// Separate channels for Radarr and Sonarr webhooks
	RadarrChannelID int64 `mapstructure:"radarr_channel_id" validate:"required"`
	SonarrChannelID int64 `mapstructure:"sonarr_channel_id" validate:"required"`

	// Message refresh interval for upload progress updates (in seconds)
	MessageRefreshInterval int `mapstructure:"message_refresh_interval" default:"5"`

	// Delay before processing webhook (in seconds) - allows filesystem to catch up for rclone mounts
	DelayTime int `mapstructure:"delay_time" default:"30"`

	// Archive format for splitting large files (rar or 7z)
	SplitArchiveFormat string `mapstructure:"split_archive_format" default:"rar"`

	// Send movie/series details message with poster image before file upload
	SendMovieDetailsMessage  bool `mapstructure:"send_movie_details_message" default:"true"`
	SendSeriesDetailsMessage bool `mapstructure:"send_series_details_message" default:"false"`

	// Archive split size in GB for splitting large files (0 = use maximum according to account type)
	// If 0, uses maximum according to account type (4GB for premium, 2GB for free)
	// If > 0, uses this value (max allowed: 4GB, supports decimals like 1.5)
	ArchiveSplitSize float64 `mapstructure:"archive_split_size" default:"0"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port" default:"8080"`
	Host string `mapstructure:"host" default:"0.0.0.0"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level" default:"info"`
	Format string `mapstructure:"format" default:"json"`
}
