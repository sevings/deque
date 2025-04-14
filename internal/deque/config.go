package deque

import (
	"errors"
	"os"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	TgToken     string `koanf:"tg_token"`
	DBPath      string `koanf:"db_path"`
	Release     bool
	AdminIDs    []int64 `koanf:"admin_ids"`
	MaxJobs     int     `koand:"max_jobs"`
	CommonText  string  `koanf:"common_text"`
	DefaultTime string  `koanf:"default_time"`
	ChatID      int64   `koanf:"chat_id"`
}

func LoadConfig() (Config, error) {
	var kConf = koanf.New("/")

	var cfg Config

	path := os.Getenv("DEQUE_CONFIG")
	if path == "" {
		path = "deque.toml"
	}

	err := kConf.Load(file.Provider(path), toml.Parser())
	if err != nil {
		return cfg, err
	}

	err = kConf.Unmarshal("", &cfg)
	if err != nil {
		return cfg, err
	}

	if cfg.TgToken == "" {
		return cfg, errors.New("telegram token is required")
	}

	return cfg, nil
}
