package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Port       int      `mapstructure:"port"`
	DaemonPort int      `mapstructure:"daemon_port"`
	BufferSize int      `mapstructure:"buffer_size"`
	IgnoreList []string `mapstructure:"ignore_list"`
	DBPath     string   `mapstructure:"db_path"`
}

var Default = Config{
	Port:       9000,
	DaemonPort: 9001,
	BufferSize: 100,
	IgnoreList: []string{".git", ".DS_Store", "*.tmp", "*.swp"},
	DBPath:     "synco.db",
}

func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home dir: %w", err)
	}

	configDir := filepath.Join(home, ".synco")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config dir: %w", err)
	}

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configDir)

	viper.SetDefault("port", Default.Port)
	viper.SetDefault("daemon_port", Default.DaemonPort)
	viper.SetDefault("buffer_size", Default.BufferSize)
	viper.SetDefault("ignore_list", Default.IgnoreList)
	viper.SetDefault("db_path", Default.DBPath)

	viper.SetEnvPrefix("SYNCO")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}
