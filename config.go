package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	TelegramToken  string `mapstructure:"telegram_token"`
	SessionToken   string `mapstructure:"session_token"`
	UserID         string `mapstructure:"user_id"`
	APIBaseURL     string `mapstructure:"api_base_url"`
	Username       string `mapstructure:"username"`
	CurrentContest string `mapstructure:"current_contest"` // Новое поле
}

func getConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sortme_plugin")
}

func LoadConfig() (*Config, error) {
	configPath := getConfigPath()

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configPath)

	// Создаем директорию если не существует
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Устанавливаем значения по умолчанию
	viper.SetDefault("api_base_url", "https://sort-me.org/api")

	// Читаем конфиг
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Создаем пустой конфиг
			if err := viper.SafeWriteConfig(); err != nil {
				return nil, fmt.Errorf("failed to create config file: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func SaveConfig(config *Config) error {
	viper.Set("telegram_token", config.TelegramToken)
	viper.Set("session_token", config.SessionToken)
	viper.Set("user_id", config.UserID)
	viper.Set("api_base_url", config.APIBaseURL)

	return viper.WriteConfig()
}
