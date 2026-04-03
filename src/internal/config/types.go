package config

type Config struct {
	Telegram TelegramConfig `mapstructure:"telegram" validate:"required"`
	Server   ServerConfig   `mapstructure:"server"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

type TelegramConfig struct {
	AppID          int    `mapstructure:"app_id" validate:"required"`
	AppHash        string `mapstructure:"app_hash" validate:"required"`
	ChannelID      int64  `mapstructure:"channel_id" validate:"required"`
	DeviceModel    string `mapstructure:"device_model" default:"Desktop"`
	SystemVersion  string `mapstructure:"system_version" default:"Windows 11"`
	AppVersion     string `mapstructure:"app_version" default:"6.7.0"`
	LangPack       string `mapstructure:"lang_pack" default:"tdesktop"`
	SystemLangCode string `mapstructure:"system_lang_code" default:"en-US"`
	LangCode       string `mapstructure:"lang_code" default:"en"`

	// Rate limiting configuration (matching teldrive architecture)
	RateLimit  bool `mapstructure:"rate_limit" default:"false"`
	Rate       int  `mapstructure:"rate" default:"100"`
	RateBurst  int  `mapstructure:"rate_burst" default:"5"`
	MaxRetries int  `mapstructure:"max_retries" default:"5"`

	// Upload performance configuration
	UploadThreads  int `mapstructure:"upload_threads" default:"8"`
	UploadPartSize int `mapstructure:"upload_part_size" default:"512"`
	PoolSize       int `mapstructure:"pool_size" default:"8"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port" default:"8080"`
	Host string `mapstructure:"host" default:"0.0.0.0"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level" default:"info"`
	Format string `mapstructure:"format" default:"json"`
}
