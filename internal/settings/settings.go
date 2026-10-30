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
	Accent     string `yaml:"accent" json:"accent"`
	Selector   string `yaml:"selector,omitempty" json:"selector,omitempty"`
	Muted      string `yaml:"muted" json:"muted"`
	Warning    string `yaml:"warning" json:"warning"`
	Text       string `yaml:"text,omitempty" json:"text,omitempty"`
	Background string `yaml:"background,omitempty" json:"background,omitempty"`
}

type Config struct {
	Version   int               `yaml:"version" json:"version"`
	Source    string            `yaml:"source" json:"source"`
	Locations []places.Location `yaml:"locations" json:"locations"`
	Colours   Colours           `yaml:"colours" json:"colours"`
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

// normaliseLegacyColours accepts the old palette key in YAML and JSON. Rename
// only the top-level mapping key; location names and other text stay untouched.
// Strict decoding below still rejects unknown fields and duplicate keys.
func normaliseLegacyColours(data []byte) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return data, nil
	}
	mapping, changed := document.Content[0], false
	for i := 0; i < len(mapping.Content); i += 2 {
		if key := mapping.Content[i]; key.Value == "colors" {
			key.Value = "colours"
			changed = true
		}
	}
	if changed {
		return yaml.Marshal(&document)
	}
	return data, nil
}

func Load(path string) (Config, error) {
	readPath := path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && filepath.Base(path) == "config.yaml" {
		// YAML accepts JSON, so old settings can be read without conversion.
		// Save still writes to config.yaml, leaving the old file intact.
		readPath = filepath.Join(filepath.Dir(path), "config.json")
		b, err = os.ReadFile(readPath)
	}
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	b, err = normaliseLegacyColours(b)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", readPath, err)
	}
	// Start only the palette with defaults: old configs need no new fields,
	// and a partial colours object overrides just the supplied colours.
	c := Config{Colours: Default().Colours}
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("read config %s: %w", readPath, err)
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
