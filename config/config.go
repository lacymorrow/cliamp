// Package config handles loading user configuration from ~/.config/cliamp/config.toml.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds user preferences loaded from the config file.
type Config struct {
	Volume   float64     // dB, range [-30, +6]
	EQ       [10]float64 // per-band gain in dB, range [-12, +12]
	EQPreset string      // preset name, or "" for custom
	Repeat   string      // "off", "all", or "one"
	Shuffle  bool
}

// Default returns a Config with sensible defaults.
func Default() Config {
	return Config{
		Repeat: "off",
	}
}

// Load reads the config file from ~/.config/cliamp/config.toml.
// Returns defaults if the file does not exist.
func Load() (Config, error) {
	cfg := Default()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, nil
	}

	path := filepath.Join(home, ".config", "cliamp", "config.toml")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "volume":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				cfg.Volume = max(min(v, 6), -30)
			}
		case "repeat":
			val = strings.Trim(val, `"'`)
			switch strings.ToLower(val) {
			case "all", "one", "off":
				cfg.Repeat = strings.ToLower(val)
			}
		case "shuffle":
			cfg.Shuffle = val == "true"
		case "eq":
			cfg.EQ = parseEQ(val)
		case "eq_preset":
			cfg.EQPreset = strings.Trim(val, `"'`)
		}
	}

	return cfg, scanner.Err()
}

// Save writes the config to ~/.config/cliamp/config.toml.
func Save(cfg Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dir := filepath.Join(home, ".config", "cliamp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	eqParts := make([]string, 10)
	for i, v := range cfg.EQ {
		eqParts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}

	content := fmt.Sprintf(`# CLIAMP configuration

# Default volume in dB (range: -30 to 6)
volume = %s

# Repeat mode: "off", "all", or "one"
repeat = "%s"

# Start with shuffle enabled
shuffle = %t

# EQ preset name (e.g. "Rock", "Jazz", "Classical", "Bass Boost")
# Leave empty or "Custom" to use the manual eq values below
eq_preset = "%s"

# 10-band EQ gains in dB (range: -12 to 12)
# Bands: 70Hz, 180Hz, 320Hz, 600Hz, 1kHz, 3kHz, 6kHz, 12kHz, 14kHz, 16kHz
# Only used when eq_preset is "Custom" or empty
eq = [%s]
`,
		strconv.FormatFloat(cfg.Volume, 'f', -1, 64),
		cfg.Repeat,
		cfg.Shuffle,
		cfg.EQPreset,
		strings.Join(eqParts, ", "),
	)

	path := filepath.Join(dir, "config.toml")
	return os.WriteFile(path, []byte(content), 0o644)
}

// parseEQ parses a TOML-style array like [0, 1.5, -2, ...] into 10 bands.
func parseEQ(val string) [10]float64 {
	var bands [10]float64
	val = strings.Trim(val, "[]")
	parts := strings.Split(val, ",")
	for i, p := range parts {
		if i >= 10 {
			break
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err == nil {
			bands[i] = max(min(v, 12), -12)
		}
	}
	return bands
}
