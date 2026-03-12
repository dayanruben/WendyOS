package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPickerModel_SelectsFromTable(t *testing.T) {
	m := NewPickerWithTitle("Select a WiFi network")

	updated, _ := m.Update(PickerAddMsg{Items: []PickerItem{
		{Name: "alpha", Type: "82%", Value: "alpha"},
		{Name: "beta", Type: "65%", Value: "beta"},
	}})
	pm := updated.(PickerModel)

	view := pm.View()
	for _, want := range []string{"Select a WiFi network", "Name", "Type", "alpha", "82%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected picker view to contain %q, got %q", want, view)
		}
	}

	updated, _ = pm.Update(tea.KeyMsg{Type: tea.KeyDown})
	pm = updated.(PickerModel)

	if pm.table.Cursor() != 1 {
		t.Fatalf("expected cursor on second row, got %d", pm.table.Cursor())
	}

	updated, cmd := pm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm = updated.(PickerModel)

	if cmd == nil {
		t.Fatal("expected enter to return quit command")
	}
	if pm.Selected() == nil {
		t.Fatal("expected selected item after enter")
	}
	if got := pm.Selected().Value.(string); got != "beta" {
		t.Fatalf("selected value = %q, want %q", got, "beta")
	}
}

func TestPickerModel_DedupesItems(t *testing.T) {
	m := NewPicker()

	updated, _ := m.Update(PickerAddMsg{Items: []PickerItem{
		{Name: "wendy-alpha", Type: "LAN", Address: "192.168.1.10", Value: "a"},
	}})
	pm := updated.(PickerModel)

	updated, _ = pm.Update(PickerAddMsg{Items: []PickerItem{
		{Name: "wendy-alpha", Type: "LAN", Address: "192.168.1.10", Value: "b"},
	}})
	pm = updated.(PickerModel)

	if got := len(pm.items); got != 1 {
		t.Fatalf("expected 1 deduped item, got %d", got)
	}
}

func TestPickerModel_ShowsDescriptionColumnWhenPresent(t *testing.T) {
	m := NewPickerWithTitle("Select a target")

	updated, _ := m.Update(PickerAddMsg{Items: []PickerItem{
		{Name: "WendyOS", Description: "Full Linux-based edge device", Value: "wendyos"},
	}})
	pm := updated.(PickerModel)

	view := pm.View()
	for _, want := range []string{"Description", "Full Linux-based edge device"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected picker view to contain %q, got %q", want, view)
		}
	}
}
