//go:build linux

package bluetooth

import (
	"fmt"

	"github.com/godbus/dbus/v5"
	"go.uber.org/zap"
)

const (
	agentObjectPath   = dbus.ObjectPath("/org/wendy/btagent")
	agentManagerIface = "org.bluez.AgentManager1"
	agentIface        = "org.bluez.Agent1"
	// agentCapability "NoInputNoOutput" selects "just works" pairing: BlueZ
	// completes pairing without prompting for a PIN or passkey, which is the
	// only viable mode on a headless device.
	agentCapability = "NoInputNoOutput"
)

// errRejected is returned for agent callbacks that should never be reached under
// NoInputNoOutput capability (PIN/passkey entry). Returning a BlueZ-recognised
// rejection lets the pairing attempt fail cleanly rather than hang.
var errRejected = dbus.NewError("org.bluez.Error.Rejected", nil)

// pairingAgent implements org.bluez.Agent1 as a headless auto-accepting agent.
// It accepts pairing and service authorization requests without user
// interaction, which is required for unattended pairing on an edge device.
type pairingAgent struct {
	logger *zap.Logger
}

// Release is called by BlueZ when the agent is unregistered.
func (a *pairingAgent) Release() *dbus.Error { return nil }

// RequestPinCode / RequestPasskey are only invoked for capabilities that can
// supply credentials. Under NoInputNoOutput they should not occur; reject them.
func (a *pairingAgent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	return "", errRejected
}

func (a *pairingAgent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	return 0, errRejected
}

// Display* callbacks are no-ops: a headless device has nothing to display.
func (a *pairingAgent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	return nil
}

func (a *pairingAgent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	return nil
}

// RequestConfirmation / RequestAuthorization / AuthorizeService auto-accept,
// completing "just works" pairing and service connections without prompting.
func (a *pairingAgent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	a.logger.Debug("Auto-confirming Bluetooth pairing", zap.String("device", string(device)))
	return nil
}

func (a *pairingAgent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	a.logger.Debug("Auto-authorizing Bluetooth pairing", zap.String("device", string(device)))
	return nil
}

func (a *pairingAgent) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	a.logger.Debug("Auto-authorizing Bluetooth service",
		zap.String("device", string(device)), zap.String("uuid", uuid))
	return nil
}

// Cancel is called by BlueZ when a request is cancelled (e.g. the remote aborts).
func (a *pairingAgent) Cancel() *dbus.Error { return nil }

// registerPairingAgent exports a headless pairing agent on conn and registers it
// with BlueZ as the default agent. The agent stays registered for the lifetime
// of conn; BlueZ unregisters it automatically when conn closes.
func registerPairingAgent(conn *dbus.Conn, logger *zap.Logger) error {
	agent := &pairingAgent{logger: logger}
	if err := conn.Export(agent, agentObjectPath, agentIface); err != nil {
		return fmt.Errorf("exporting pairing agent: %w", err)
	}

	mgr := conn.Object(bluezService, "/org/bluez")
	if call := mgr.Call(agentManagerIface+".RegisterAgent", 0, agentObjectPath, agentCapability); call.Err != nil {
		return fmt.Errorf("registering pairing agent: %w", call.Err)
	}
	// Become the default agent so BlueZ routes authentication requests here even
	// when other agents (e.g. a stray bluetoothctl) are present.
	if call := mgr.Call(agentManagerIface+".RequestDefaultAgent", 0, agentObjectPath); call.Err != nil {
		return fmt.Errorf("requesting default pairing agent: %w", call.Err)
	}

	return nil
}
