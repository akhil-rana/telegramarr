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
	AppVersion     string `mapstructure:"app_version" default:"6.7.1"`
	LangPack       string `mapstructure:"lang_pack" default:"tdesktop"`
	SystemLangCode string `mapstructure:"system_lang_code" default:"en-US"`
	LangCode       string `mapstructure:"lang_code" default:"en"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port" default:"8080"`
	Host string `mapstructure:"host" default:"0.0.0.0"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level" default:"info"`
	Format string `mapstructure:"format" default:"json"`
}
