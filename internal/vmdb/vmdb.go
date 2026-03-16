package vmdb

import (
	"encoding/json"
	"fmt"
	"os"
)

type VM struct {
	Name          string  `json:"vm_name"`
	// VMID is int for Proxmox, string (domain name) for libvirt.
	VMID          any     `json:"vmid"`
	IPAddress     string  `json:"ip_address"`
	Status        string  `json:"status"`
	Owner         string  `json:"owner"`
	WalletAddress *string `json:"wallet_address"`
	CreatedAt     string  `json:"created_at"`
	ExpiresAt     string  `json:"expires_at"`
}

type database struct {
	VMs map[string]VM `json:"vms"`
}

// LoadActiveVMs reads vms.json and returns only VMs with status "active".
// The returned slice has no guaranteed order.
func LoadActiveVMs(path string) ([]VM, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vm database: %w", err)
	}
	var db database
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("parse vm database: %w", err)
	}

	var active []VM
	for _, vm := range db.VMs {
		if vm.Status == "active" {
			active = append(active, vm)
		}
	}
	return active, nil
}
