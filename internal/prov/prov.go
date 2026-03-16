package prov

import (
	"encoding/json"
	"fmt"
	"os"
)

// Manifest holds the subset of the provisioner manifest the monitor needs.
// The full schema (setup, root_agent_actions, config_keys) is in
// PROVISIONER_INTERFACE.md section 1.
type Manifest struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Version     string            `json:"version"`
	Commands    map[string]string `json:"commands"`
}

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read provisioner manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse provisioner manifest: %w", err)
	}
	return &m, nil
}

func (m *Manifest) GetCommand(verb string) (string, error) {
	cmd, ok := m.Commands[verb]
	if !ok {
		return "", fmt.Errorf("unknown provisioner verb: %q", verb)
	}
	return cmd, nil
}
