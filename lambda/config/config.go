package config

import (
	"path/filepath"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	BaseDir      string `env:"APT_BASE_DIR"`
	Distribution string `env:"APT_DISTRIBUTION"`

	Origin      string `env:"APT_ORIGIN"`
	Label       string `env:"APT_LABEL"`
	Suite       string `env:"APT_SUITE"`
	CodeName    string `env:"APT_CODENAME"`
	Components  string `env:"APT_COMPONENTS"`
	Description string `env:"APT_DESCRIPTION"`

	PrivateKeyS3Url string `env:"APT_PRIVATE_KEY_S3URL"`
	LockKeyS3Url    string `env:"APT_LOCK_KEY_S3URL"`
	DestS3Bucket    string `env:"APT_S3BUCKET"`
}

func Load() (Config, error) {
	return env.ParseAs[Config]()
}

func (cfg *Config) DistributionDirName() string {
	return filepath.Join(cfg.BaseDir, "dists", cfg.Distribution)
}

func (cfg *Config) DirName() string {
	return filepath.Join(cfg.BaseDir, "dists", cfg.Distribution, cfg.Components)
}
