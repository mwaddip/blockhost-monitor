package prov

import (
	"testing"
)

func TestLoadManifest(t *testing.T) {
	m, err := LoadManifest("../../testdata/provisioner.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "libvirt" {
		t.Errorf("name = %q, want %q", m.Name, "libvirt")
	}
}

func TestGetCommand(t *testing.T) {
	m, err := LoadManifest("../../testdata/provisioner.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	cmd, err := m.GetCommand("metrics")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "blockhost-vm-metrics" {
		t.Errorf("metrics command = %q, want %q", cmd, "blockhost-vm-metrics")
	}
}

func TestGetCommand_Unknown(t *testing.T) {
	m, err := LoadManifest("../../testdata/provisioner.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	_, err = m.GetCommand("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown verb")
	}
}

func TestLoadManifest_FileNotFound(t *testing.T) {
	_, err := LoadManifest("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
