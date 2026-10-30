// Package settings persists the user's named locations and default source.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"meetlet/internal/places"
)

type Config struct {
	Version   int               `json:"version"`
	Source    string            `json:"source"`
	Locations []places.Location `json:"locations"`
}

func Default() Config { return Config{Version: 1, Source: "London", Locations: places.Defaults()} }

func Path() (string, error) {
	if p := os.Getenv("MEETLET_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "meetlet", "config.json"), nil
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("read config %s: %w", path, err)
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if len(c.Locations) == 0 {
		return fmt.Errorf("keep at least one location")
	}
	seen, hasSource := map[string]bool{}, false
	for _, p := range c.Locations {
		if err := p.Validate(); err != nil {
			return err
		}
		key := strings.ToLower(p.Name)
		if seen[key] {
			return fmt.Errorf("duplicate location name %q", p.Name)
		}
		seen[key] = true
		hasSource = hasSource || p.Name == c.Source
	}
	if !hasSource {
		return fmt.Errorf("source %q is missing from saved locations", c.Source)
	}
	return nil
}

func (c Config) Origin() places.Location {
	for _, p := range c.Locations {
		if p.Name == c.Source {
			return p
		}
	}
	return places.Location{}
}

// Save replaces the file atomically so an interrupted write cannot truncate it.
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".meetlet-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
