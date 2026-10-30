package settings

import (
	"os"
	"path/filepath"
	"testing"
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
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("reading defaults should not create a file")
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	c.Source = "Bonn"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Source != "Bonn" {
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

func TestCorruptConfigIsNotSilentlyReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, content := range []string{`{broken`, `{"version":99}`, `{"version":1,"source":"London","locations":[{"name":"London","zone":"Invalid/Zone"}]}`} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
}
