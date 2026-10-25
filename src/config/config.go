package config

import (
	"log"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Logging struct {
		Level string `toml:"level"`
	} `toml:"logging"`
	Database struct {
		Host     string `toml:"host"`
		Port     int    `toml:"port"`
		User     string `toml:"user"`
		Password string `toml:"password"`
		Database string `toml:"database"`
		SSLMode  string `toml:"sslmode"`
	} `toml:"database"`

	Auth AuthConfig `toml:"auth"`
}

type AuthConfig struct {
	DisableFusionAuth  bool   `toml:"disable_fusionauth"`
	FusionAuthURL      string `toml:"fusionauth_url"`
	FusionAuthClientId string `toml:"fusionauth_client_id"`
	FusionAuthSecret   string `toml:"fusionauth_client_secret"`
	FusionAuthTenantId string `toml:"fusionauth_tenant_id"`
	OAuthRedirectURI   string `toml:"oauth_redirect_uri"`
	FrontendURL        string `toml:"frontend_url"`
	RequiredRole       string `toml:"required_role"`
}

func GetConfig() *Config {
	var cfg Config

	path := os.Getenv("RCJV_CONFIG")
	if path == "" {
		path = "config.toml"
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Error opening config file: %v\n", err)
	}

	if err := toml.Unmarshal(bytes, &cfg); err != nil {
		log.Fatalf("Error unmarshaling config file: %v\n", err)
	}

	return &cfg
}
