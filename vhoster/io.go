package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func SaveConfig(config *Config) {
	payload, err := json.MarshalIndent(config, "", "	")
	if err != nil {
		panic(fmt.Errorf("encode configuration for config.json: %w", err))
	}
	if err := os.WriteFile("config.json", payload, 0600); err != nil {
		panic(fmt.Errorf("write configuration config.json: %w", err))
	}
}

// Load user data
func LoadConfig(file string) *Config {
	config := Config{LogFile: "accessLog"}
	raw, err := os.ReadFile(file)
	if err != nil {
		panic(fmt.Errorf("read configuration %q: %w", file, err))
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		panic(fmt.Errorf("decode configuration %q: %w", file, err))
	}
	if len(config.Redirects) == 0 {
		panic(fmt.Errorf("configuration %q must include at least one redirect", file))
	}
	for index, redirect := range config.Redirects {
		if requestHostname(redirect.Host) == "" {
			panic(fmt.Errorf("configuration %q redirect %d must include a nonempty Host", file, index))
		}
		if _, err := parseTarget(redirect.To); err != nil {
			panic(fmt.Errorf("configuration %q redirect %d: %w", file, index, err))
		}
	}
	return &config
}
