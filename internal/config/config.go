package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server    ServerConfig    `toml:"server"`
	Retention RetentionConfig `toml:"retention"`
}

type ServerConfig struct {
	Port        int    `toml:"port"`
	UploadsDir  string `toml:"uploads_dir"`
	VncBasePort int    `toml:"vnc_base_port"`
}

type RetentionConfig struct {
	AuditDays int `toml:"audit_days"`
	ScrubDays int `toml:"scrub_days"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:        8080,
			UploadsDir:  "./uploads",
			VncBasePort: 5900,
		},
		Retention: RetentionConfig{
			AuditDays: 365,
			ScrubDays: 30,
		},
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
