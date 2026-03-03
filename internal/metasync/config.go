package metasync

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	secretService = "ente"
	secretUser    = "ente-cli-user"
)

// Config holds the ente CLI configuration
type Config struct {
	APIEndpoint string
}

// LoadConfig loads the ente CLI configuration from ~/.ente/config.yaml
func LoadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, ".ente", "config.yaml")

	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		// Config file may not exist, use defaults
		return &Config{
			APIEndpoint: "https://api.ente.io",
		}, nil
	}

	return &Config{
		APIEndpoint: viper.GetString("endpoint.api"),
	}, nil
}

// GetEnteCLIConfigDir returns the path to the .ente directory
func GetEnteCLIConfigDir() (string, error) {
	if configDir := os.Getenv("ENTE_CLI_CONFIG_DIR"); configDir != "" {
		return configDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".ente"), nil
}