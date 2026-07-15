package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds the application configuration.
type Config struct {
	CachePath          string `toml:"cache_path"`
	ConfirmBeforeCache bool   `toml:"confirm_before_cache"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		CachePath:          "data/cache",
		ConfirmBeforeCache: true,
	}
}

// LoadConfig reads the config from vidcache.toml next to the binary,
// or returns defaults if the file doesn't exist.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()

	// Look for config next to executable first, then current directory
	paths := []string{}

	exe, err := os.Executable()
	if err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "vidcache.toml"))
	}
	paths = append(paths, "vidcache.toml")

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			if _, err := toml.DecodeFile(p, &cfg); err != nil {
				return cfg, fmt.Errorf("parsing config %s: %w", p, err)
			}
			return cfg, nil
		}
	}

	return cfg, nil
}
