package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"meetlet/internal/places"
)

func TestDefaultAndSafePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Locations) != 4 || c.Origin().Name != "London" {
		t.Fatal(c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("invalid shipped defaults: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("reading defaults should not create a file")
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	c.Source = "Bonn"
	c.Colours.Accent = "#123456"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Source != "Bonn" || loaded.Colours != c.Colours {
		t.Fatalf("%+v %v", loaded, err)
	}
	c.Source = "Missing"
	if err := Save(path, c); err == nil {
		t.Fatal("accepted missing source")
	}
	loaded, _ = Load(path)
	if loaded.Source != "Bonn" {
		t.Fatal("failed save changed the file")
	}
}

func TestIndependentDefaults(t *testing.T) {
	c := Default()
	c.Locations[0].Name = "Changed"
	c.Colours.Accent = "#000000"
	if next := Default(); next.Locations[0].Name != "London" || next.Colours.Accent == c.Colours.Accent {
		t.Fatal("default configurations share mutable state")
	}
}

func TestCustomLocationsAndColours(t *testing.T) {
	c := Default()
	c.Source = "Tokyo"
	c.Locations = []places.Location{{Name: "Tokyo", Zone: "Asia/Tokyo"}, {Name: "Office", Zone: "Europe/Paris"}}
	c.Colours = Colours{Accent: "#A9B665", Selector: "#EA6962", Muted: "#928374", Warning: "#D8A657", Text: "#D4BE98", Background: "#282828"}
	path := filepath.Join(t.TempDir(), "team.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || !reflect.DeepEqual(loaded, c) {
		t.Fatalf("custom configuration did not round-trip: %+v, %v", loaded, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || json.Valid(content) || !strings.Contains(string(content), "colours:\n") {
		t.Fatalf("config was not saved as YAML: %s, %v", content, err)
	}
}

func TestYAMLWithCommentsAndPartialColours(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `# My meeting planner
version: 1
source: Office
locations:
  - name: Office
    zone: Asia/Tokyo
colours:
  accent: "#A9B665" # Gruvbox Material green
  text: "#D4BE98"
  background: "#282828"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.Source != "Office" || len(c.Locations) != 1 || c.Locations[0].Zone != "Asia/Tokyo" {
		t.Fatalf("YAML locations were not read: %+v, %v", c, err)
	}
	if c.Colours.Accent != "#A9B665" || c.Colours.Text != "#D4BE98" || c.Colours.Background != "#282828" || c.Colours.Warning != Default().Colours.Warning {
		t.Fatalf("YAML palette was not merged: %+v", c.Colours)
	}
}

func TestLegacyJSONMigration(t *testing.T) {
	dir := t.TempDir()
	legacy, path := filepath.Join(dir, "config.json"), filepath.Join(dir, "config.yaml")
	old := `{"version":1,"source":"Office","locations":[{"name":"Office","zone":"Asia/Tokyo"}]}`
	if err := os.WriteFile(legacy, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.Source != "Office" || c.Locations[0].Zone != "Asia/Tokyo" {
		t.Fatalf("legacy locations lost: %+v, %v", c, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("loading should not create files")
	}
	c.Colours.Accent = "#A9B665"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(legacy)
	if err != nil || string(before) != old {
		t.Fatal("the legacy JSON was modified")
	}
	// The new YAML must take precedence over the old JSON on later launches.
	loaded, err := Load(path)
	if err != nil || !reflect.DeepEqual(loaded, c) {
		t.Fatalf("YAML config was not preferred: %+v, %v", loaded, err)
	}
}

func TestConfigPath(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("XDG path checks apply to macOS and Linux")
	}
	t.Setenv("MEETLET_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, err := Path(); err != nil || got != filepath.Join(home, ".config", "meetlet", "config.yaml") {
		t.Fatalf("default YAML path: %q, %v", got, err)
	}
	xdg := filepath.Join(t.TempDir(), "custom-config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got, err := Path(); err != nil || got != filepath.Join(xdg, "meetlet", "config.yaml") {
		t.Fatalf("XDG override: %q, %v", got, err)
	}
	// An explicit config takes precedence over XDG_CONFIG_HOME.
	t.Setenv("MEETLET_CONFIG", filepath.Join(t.TempDir(), "team.yaml"))
	if got, err := Path(); err != nil || got != os.Getenv("MEETLET_CONFIG") {
		t.Fatalf("config override: %q, %v", got, err)
	}
	t.Setenv("MEETLET_CONFIG", "")
	t.Setenv("HOME", "")
	if got, err := Path(); err != nil || got != filepath.Join(xdg, "meetlet", "config.yaml") {
		t.Fatalf("XDG should work without HOME: %q, %v", got, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := Path(); err == nil {
		t.Fatal("missing HOME and XDG_CONFIG_HOME should report an error")
	}
}

func TestLegacyAndPartialColours(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, extra := range []string{"", `,"colours":{"accent":"#123456"}`, `,"colors":{"accent":"#123456"}`} {
		content := `{"version":1,"source":"Office","locations":[{"name":"Office","zone":"Asia/Tokyo"}]` + extra + `}`
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		want := Default().Colours
		if extra != "" {
			want.Accent = "#123456"
		}
		if c.Colours != want || c.Source != "Office" || len(c.Locations) != 1 || c.Locations[0].Zone != "Asia/Tokyo" {
			t.Fatalf("defaults changed saved settings: %+v", c)
		}
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		after, err := Load(path)
		if err != nil || !reflect.DeepEqual(after, c) {
			t.Fatalf("saving lost configured colours: %+v, %v", after, err)
		}
	}
}

func TestLegacyPaletteKeySavesWithUKSpelling(t *testing.T) {
	for _, oldKey := range []string{"colors", "colours"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		// The name is ordinary data and must not be changed by key migration.
		content := "version: 1\nsource: colors\nlocations:\n  - name: colors\n    zone: Europe/London\n" +
			oldKey + ":\n  accent: \"#A9B665\"\n  selector: \"#EA6962\"\n"
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil || c.Source != "colors" || c.Locations[0].Name != "colors" || c.Colours.Selector != "#EA6962" {
			t.Fatalf("loading %s lost settings: %+v, %v", oldKey, c, err)
		}
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		saved, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(saved), "\ncolours:\n") || strings.Contains(string(saved), "\ncolors:\n") {
			t.Fatalf("config did not save with UK spelling: %s, %v", saved, err)
		}
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "version: 1\nsource: London\nlocations:\n  - name: London\n    zone: Europe/London\ncolors: {}\ncolours: {}\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("both palette spellings must not silently override each other")
	}
}

func TestInvalidColoursDoNotOverwriteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "red", "#fff", "#GG1122", "#1234567", "123456"} {
		c := Default()
		c.Colours.Accent = invalid
		if err := Save(path, c); err == nil || !strings.Contains(err.Error(), "colours.accent") {
			t.Fatalf("invalid colour %q: %v", invalid, err)
		}
		content, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		badPath := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(badPath, content, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(badPath); err == nil {
			t.Fatalf("loaded invalid colour %q", invalid)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("invalid colour changed the saved config")
	}
}

func TestInvalidSelectorColour(t *testing.T) {
	c := Default()
	c.Colours.Selector = "red"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "colours.selector") {
		t.Fatalf("invalid selector colour: %v", err)
	}
}

func TestCorruptConfigIsNotSilentlyReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, content := range []string{`{broken`, `{"version":99}`, `{"version":1,"source":"London","locations":[{"name":"London","zone":"Invalid/Zone"}]}`, "version: 1\ncolours: {}\n", "version: 1\nversion: 2\n"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
}
