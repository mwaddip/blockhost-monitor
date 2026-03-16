package vmdb

import (
	"os"
	"testing"
)

func TestLoadActiveVMs(t *testing.T) {
	vms, err := LoadActiveVMs("../../testdata/vms.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// vms.json has 2 active (web1, db1) and 1 suspended (old1)
	if len(vms) != 2 {
		t.Fatalf("got %d active VMs, want 2", len(vms))
	}

	// Map iteration order is non-deterministic — check by name
	names := map[string]bool{}
	for _, vm := range vms {
		names[vm.Name] = true
	}
	if !names["web1"] {
		t.Error("missing active VM: web1")
	}
	if !names["db1"] {
		t.Error("missing active VM: db1")
	}
	if names["old1"] {
		t.Error("suspended VM old1 should not be in active list")
	}
}

func TestLoadActiveVMs_FileNotFound(t *testing.T) {
	_, err := LoadActiveVMs("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadActiveVMs_EmptyDB(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.json"
	if err := os.WriteFile(path, []byte(`{"vms":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	vms, err := LoadActiveVMs(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vms) != 0 {
		t.Errorf("got %d VMs, want 0", len(vms))
	}
}
