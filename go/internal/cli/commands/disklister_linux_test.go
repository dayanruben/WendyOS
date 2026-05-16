//go:build linux

package commands

import (
	"encoding/json"
	"testing"
)

// ── flexBool ────────────────────────────────────────────────────────

func TestFlexBool(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"bool true", `true`, true},
		{"bool false", `false`, false},
		{"string 1", `"1"`, true},
		{"string 0", `"0"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got flexBool
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) error: %v", tt.input, err)
			}
			if bool(got) != tt.want {
				t.Fatalf("Unmarshal(%s) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFlexBoolRejectsInvalidInput(t *testing.T) {
	var got flexBool
	if err := json.Unmarshal([]byte(`[]`), &got); err == nil {
		t.Fatal("expected error for array input, got nil")
	}
}

// ── lsblk JSON parsing ─────────────────────────────────────────────

func TestParseLsblkOutput(t *testing.T) {
	tests := []struct {
		name          string
		json          string
		wantDevices   int
		wantName      string
		wantRemovable bool
	}{
		{
			name: "string fields (older lsblk)",
			json: `{
				"blockdevices": [{
					"name": "sda",
					"size": "256060514304",
					"type": "disk",
					"rm": "1",
					"hotplug": "1",
					"tran": "usb",
					"mountpoint": null
				}]
			}`,
			wantDevices:   1,
			wantName:      "sda",
			wantRemovable: true,
		},
		{
			name: "bool fields (newer lsblk)",
			json: `{
				"blockdevices": [{
					"name": "sda",
					"size": "256060514304",
					"type": "disk",
					"rm": true,
					"hotplug": true,
					"tran": "usb",
					"mountpoint": null
				}]
			}`,
			wantDevices:   1,
			wantName:      "sda",
			wantRemovable: true,
		},
		{
			name: "non-removable bool",
			json: `{
				"blockdevices": [{
					"name": "nvme0n1",
					"size": "1000204886016",
					"type": "disk",
					"rm": false,
					"hotplug": false,
					"tran": "nvme",
					"mountpoint": null
				}]
			}`,
			wantDevices:   1,
			wantName:      "nvme0n1",
			wantRemovable: false,
		},
		{
			name: "non-removable string",
			json: `{
				"blockdevices": [{
					"name": "nvme0n1",
					"size": "1000204886016",
					"type": "disk",
					"rm": "0",
					"hotplug": "0",
					"tran": "nvme",
					"mountpoint": null
				}]
			}`,
			wantDevices:   1,
			wantName:      "nvme0n1",
			wantRemovable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result lsblkOutput
			if err := json.Unmarshal([]byte(tt.json), &result); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if len(result.Blockdevices) != tt.wantDevices {
				t.Fatalf("got %d devices, want %d", len(result.Blockdevices), tt.wantDevices)
			}
			dev := result.Blockdevices[0]
			if dev.Name != tt.wantName {
				t.Fatalf("Name = %q, want %q", dev.Name, tt.wantName)
			}
			if bool(dev.Removable) != tt.wantRemovable {
				t.Fatalf("Removable = %v, want %v", dev.Removable, tt.wantRemovable)
			}
		})
	}
}

// TestParseLsblkChildrenUnmarshaled verifies that the Children field is populated
// when lsblk emits hierarchical JSON (without -l), so that unmountLsblkDevice
// can recurse into nested partitions (fix for bug_2683497e).
func TestParseLsblkChildrenUnmarshaled(t *testing.T) {
	const hierarchical = `{
		"blockdevices": [
			{
				"name": "sdb",
				"size": "16000000000",
				"type": "disk",
				"rm": true,
				"hotplug": true,
				"tran": "usb",
				"mountpoint": null,
				"children": [
					{
						"name": "sdb1",
						"size": "536870912",
						"type": "part",
						"rm": true,
						"hotplug": true,
						"tran": "usb",
						"mountpoint": "/boot/efi"
					},
					{
						"name": "sdb2",
						"size": "15463129088",
						"type": "part",
						"rm": true,
						"hotplug": true,
						"tran": "usb",
						"mountpoint": "/media/user/data"
					}
				]
			}
		]
	}`

	var result lsblkOutput
	if err := json.Unmarshal([]byte(hierarchical), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(result.Blockdevices) != 1 {
		t.Fatalf("got %d top-level devices, want 1", len(result.Blockdevices))
	}
	disk := result.Blockdevices[0]
	if len(disk.Children) != 2 {
		t.Fatalf("got %d children, want 2 — Children field not unmarshaled (this is the root cause of bug_2683497e)", len(disk.Children))
	}
	if disk.Children[0].Name != "sdb1" {
		t.Fatalf("Children[0].Name = %q, want \"sdb1\"", disk.Children[0].Name)
	}
	if disk.Children[1].Mountpoint != "/media/user/data" {
		t.Fatalf("Children[1].Mountpoint = %q, want \"/media/user/data\"", disk.Children[1].Mountpoint)
	}
}

// TestParseLsblkOutputMatchesLinuxReport reproduces the exact lsblk -J output
// from the bug report (WDY-774) to verify it parses without error.
func TestParseLsblkOutputMatchesLinuxReport(t *testing.T) {
	// Trimmed version of the real output from the bug report: one USB disk
	// (sda) with boolean rm/hotplug fields and one NVMe disk.
	const reported = `{
		"blockdevices": [
			{
				"name": "sda",
				"size": "256060514304",
				"type": "disk",
				"rm": false,
				"hotplug": false,
				"tran": "usb",
				"mountpoint": null
			},
			{
				"name": "nvme2n1",
				"size": "2000398934016",
				"type": "disk",
				"rm": false,
				"hotplug": false,
				"tran": "nvme",
				"mountpoint": null
			}
		]
	}`

	var result lsblkOutput
	if err := json.Unmarshal([]byte(reported), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(result.Blockdevices) != 2 {
		t.Fatalf("got %d devices, want 2", len(result.Blockdevices))
	}
}
