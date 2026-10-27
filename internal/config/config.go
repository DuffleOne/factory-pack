// Package config keeps the tool's choices between runs, in a JSON file in
// the system's place for such things. Every setting can be left out, and
// then the tool works it out.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Config is everything the tool can be told.
type Config struct {
	// Server is the factory planner to send packs to.
	Server string `json:"server,omitempty"`
	// Binary, Data and User are the game's executable, the folder holding
	// base and core, and the folder holding the player's mods. Empty is
	// looked for.
	Binary string `json:"binary,omitempty"`
	Data   string `json:"data,omitempty"`
	User   string `json:"user,omitempty"`
	// Mods are the mods to load, by name. Empty is what the game has
	// switched on.
	Mods []string `json:"mods,omitempty"`
	// Exact loads only Mods, without adding what they need.
	Exact bool `json:"exact,omitempty"`
	// Settings is "mine", "defaults", or the path of a mod-settings.dat.
	Settings string `json:"settings,omitempty"`
	// Locale is the language to name things in, as the game spells it:
	// "en", "de", "pt-BR". Empty is the game's own choice.
	Locale string `json:"locale,omitempty"`
	// Name is what the pack is called in the planner.
	Name string `json:"name,omitempty"`
	// Keep is a folder to keep a copy of everything sent in.
	Keep string `json:"keep,omitempty"`
	// Work is where the game runs, the system's temporary folder if empty.
	Work string `json:"work,omitempty"`
	// Timeout is the longest one run of the game may take, as "15m".
	Timeout string `json:"timeout,omitempty"`
}

// DefaultTimeout is how long one run of the game gets. A lot of mods take
// a minute or two to load.
const DefaultTimeout = 15 * time.Minute

// RunTimeout is Timeout read, or the default for one that's missing or
// doesn't read.
func (c *Config) RunTimeout() time.Duration {
	if d, err := time.ParseDuration(c.Timeout); err == nil && d > 0 {
		return d
	}

	return DefaultTimeout
}

// Path is where the config file lives.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "factory-pack", "config.json"), nil
}

// Load reads the config at path, or nothing at all for a file that isn't
// there yet.
func Load(path string) (*Config, error) {
	c := &Config{}

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}

	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}

	return c, nil
}

// Save writes c to path, making its folder if it has to.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(b, '\n'), 0o644)
}
