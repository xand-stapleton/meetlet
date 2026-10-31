// Package settings persists colours, named locations, and the default source.
package settings

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"meetlet/internal/places"
)

//go:embed defaults.yaml
var defaultYAML []byte

// Colours names the UI colour roles. Empty Text and Background values leave
// those colours to the terminal. Empty Selector falls back to Accent.
type Colours struct {
	Accent     string `yaml:"accent"`
	Selector   string `yaml:"selector,omitempty"`
	Muted      string `yaml:"muted"`
	Warning    string `yaml:"warning"`
	Text       string `yaml:"text,omitempty"`
	Background string `yaml:"background,omitempty"`
}

type Config struct {
	Version   int               `yaml:"version"`
	Source    string            `yaml:"source"`
	Locations []places.Location `yaml:"locations"`
	Colours   Colours           `yaml:"colours"`
}

// Default returns an independent copy of the shipped configuration. Embedding
// the YAML keeps installed binaries independent of the source tree.
func Default() Config {
	var c Config
	if err := yaml.Unmarshal(defaultYAML, &c); err != nil {
		panic("invalid embedded default configuration: " + err.Error())
	}
	return c
}

func Path() (string, error) {
	if p := os.Getenv("MEETLET_CONFIG"); p != "" {
		return p, nil
	}
	var dir string
	var err error
	if runtime.GOOS == "darwin" {
		// Use the same XDG convention as Linux on macOS too.
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir, err = os.UserHomeDir()
			dir = filepath.Join(dir, ".config")
		}
	} else {
		dir, err = os.UserConfigDir()
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "meetlet", "config.yaml"), nil
}

// Load reads one YAML file, or uses the shipped defaults if it does not exist.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	// Omitted colours keep their defaults; locations come from this file.
	c := Config{Colours: Default().Colours}
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("read config %s: %w", path, err)
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	for _, colour := range []struct{ name, value string }{
		{"accent", c.Colours.Accent}, {"muted", c.Colours.Muted}, {"warning", c.Colours.Warning},
		{"selector", c.Colours.Selector},
		{"text", c.Colours.Text}, {"background", c.Colours.Background},
	} {
		if colour.value == "" && (colour.name == "selector" || colour.name == "text" || colour.name == "background") {
			continue
		}
		if len(colour.value) != 7 || colour.value[0] != '#' {
			return fmt.Errorf("colours.%s must be a hex colour such as #A6E3A1", colour.name)
		}
		if _, err := hex.DecodeString(colour.value[1:]); err != nil {
			return fmt.Errorf("colours.%s must be a hex colour such as #A6E3A1", colour.name)
		}
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
	var b bytes.Buffer
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(c); err != nil {
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
	if _, err := f.Write(b.Bytes()); err != nil {
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
