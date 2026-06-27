package commands

import (
	"testing"
)

func TestTourAdvanceOffersCompletionsWhenMissing(t *testing.T) {
	withTourCompletionsInstalled(t, false)
	m := newTourWizardModel()
	// No AI tools detected funnels through advanceToDeviceSetup.
	got, _ := stepTour(t, m, tourAICheckDoneMsg{})
	if got.phase != phaseCompletions {
		t.Fatalf("phase = %v, want phaseCompletions", got.phase)
	}
	if got.menuCursor != 0 {
		t.Errorf("menuCursor = %d, want 0", got.menuCursor)
	}
}

func TestTourAdvanceSkipsCompletionsWhenInstalled(t *testing.T) {
	withTourCompletionsInstalled(t, true)
	m := newTourWizardModel()
	got, cmd := stepTour(t, m, tourAICheckDoneMsg{})
	if got.phase != phaseLoadDevices {
		t.Fatalf("phase = %v, want phaseLoadDevices", got.phase)
	}
	if cmd == nil {
		t.Error("expected loadDevicesCmd")
	}
}

func TestTourCompletionsPhaseYesInstalls(t *testing.T) {
	m := newTourWizardModel()
	m.phase = phaseCompletions
	m.menuCursor = 0
	got, cmd := stepTour(t, m, key("enter"))
	// Stays on the phase until the install subprocess reports back.
	if got.phase != phaseCompletions {
		t.Errorf("phase = %v, want phaseCompletions (awaiting install)", got.phase)
	}
	if cmd == nil {
		t.Error("expected an install command")
	}
}

func TestTourCompletionsPhaseNoSkips(t *testing.T) {
	m := newTourWizardModel()
	m.phase = phaseCompletions
	m.menuCursor = 1
	got, cmd := stepTour(t, m, key("enter"))
	if got.phase != phaseLoadDevices {
		t.Fatalf("phase = %v, want phaseLoadDevices", got.phase)
	}
	if cmd == nil {
		t.Error("expected loadDevicesCmd")
	}
}

func TestTourCompletionInstallDoneProceeds(t *testing.T) {
	m := newTourWizardModel()
	m.phase = phaseCompletions
	got, cmd := stepTour(t, m, tourCompletionInstallDoneMsg{})
	if got.phase != phaseLoadDevices {
		t.Fatalf("phase = %v, want phaseLoadDevices", got.phase)
	}
	if cmd == nil {
		t.Error("expected loadDevicesCmd")
	}
	// Even on install error, the tour proceeds (completions are optional).
	got, _ = stepTour(t, m, tourCompletionInstallDoneMsg{err: errTest})
	if got.phase != phaseLoadDevices {
		t.Errorf("phase = %v, want phaseLoadDevices on install error", got.phase)
	}
}
