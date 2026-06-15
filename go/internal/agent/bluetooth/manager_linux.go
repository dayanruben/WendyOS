//go:build linux

package bluetooth

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"go.uber.org/zap"

	agentpb "github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

const (
	adapterIface = "org.bluez.Adapter1"
	deviceIface  = "org.bluez.Device1"
	// scanDuration is how long discovery runs before results are collected.
	scanDuration = 8 * time.Second
)

type BlueZManager struct {
	logger *zap.Logger
}

func newPlatformManager(logger *zap.Logger) Manager {
	return &BlueZManager{logger: logger}
}

// Scan runs a Bluetooth discovery via BlueZ over D-Bus and returns the
// discovered devices on the channel. It powers on the adapter, runs discovery
// for scanDuration, then enumerates known devices through the BlueZ
// ObjectManager so typed properties (RSSI, paired/connected/trusted, icon) are
// read directly rather than parsed from bluetoothctl text output.
func (m *BlueZManager) Scan(ctx context.Context) (<-chan []*agentpb.DiscoveredBluetoothPeripheral, error) {
	ch := make(chan []*agentpb.DiscoveredBluetoothPeripheral, 10)

	go func() {
		defer close(ch)

		conn, err := dbus.ConnectSystemBus()
		if err != nil {
			m.logger.Warn("Failed to connect to system bus for Bluetooth scan", zap.Error(err))
			return
		}
		defer conn.Close()

		adapterPath := bluezAdapterPath()
		adapter := conn.Object(bluezService, dbus.ObjectPath(adapterPath))

		// Power on the adapter. The call is a no-op if it is already on, but it
		// also clears Command Disallowed state left over from a previous BLE
		// connection that wasn't fully torn down at the HCI level.
		if call := adapter.Call("org.freedesktop.DBus.Properties.Set", 0,
			adapterIface, "Powered", dbus.MakeVariant(true)); call.Err != nil {
			m.logger.Warn("Failed to power on Bluetooth adapter", zap.Error(call.Err))
			return
		}

		// Start discovery.
		if call := adapter.Call(adapterIface+".StartDiscovery", 0); call.Err != nil {
			m.logger.Warn("Failed to start Bluetooth discovery", zap.Error(call.Err))
			return
		}

		// Let discovery run, then stop it (best-effort).
		select {
		case <-time.After(scanDuration):
		case <-ctx.Done():
		}
		if call := adapter.Call(adapterIface+".StopDiscovery", 0); call.Err != nil {
			m.logger.Debug("Failed to stop Bluetooth discovery", zap.Error(call.Err))
		}

		peripherals := m.collectPeripherals(conn, adapterPath)
		if len(peripherals) > 0 {
			select {
			case ch <- peripherals:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}

// collectPeripherals enumerates devices known to BlueZ via the ObjectManager
// and returns those belonging to the given adapter.
func (m *BlueZManager) collectPeripherals(conn *dbus.Conn, adapterPath string) []*agentpb.DiscoveredBluetoothPeripheral {
	var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	root := conn.Object(bluezService, "/")
	if err := root.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&managed); err != nil {
		m.logger.Warn("Failed to enumerate Bluetooth devices", zap.Error(err))
		return nil
	}

	// Device object paths are nested under the adapter, e.g.
	// /org/bluez/hci0/dev_XX_XX_XX_XX_XX_XX.
	prefix := adapterPath + "/"
	var peripherals []*agentpb.DiscoveredBluetoothPeripheral
	for path, ifaces := range managed {
		props, ok := ifaces[deviceIface]
		if !ok || !strings.HasPrefix(string(path), prefix) {
			continue
		}
		peripherals = append(peripherals, deviceFromProps(props))
	}
	return peripherals
}

// deviceFromProps maps org.bluez.Device1 properties to the proto peripheral.
func deviceFromProps(props map[string]dbus.Variant) *agentpb.DiscoveredBluetoothPeripheral {
	p := &agentpb.DiscoveredBluetoothPeripheral{}

	if s, ok := stringProp(props, "Address"); ok {
		p.Address = s
	}
	// Alias is the user-facing name (falls back to Name when unset by BlueZ).
	if s, ok := stringProp(props, "Alias"); ok && s != "" {
		p.Name = s
	} else if s, ok := stringProp(props, "Name"); ok {
		p.Name = s
	}
	if v, ok := props["RSSI"]; ok {
		if rssi, ok := v.Value().(int16); ok {
			p.Rssi = int32(rssi)
		}
	}
	// BlueZ icons for audio devices look like "audio-headset" / "audio-card".
	if icon, ok := stringProp(props, "Icon"); ok && strings.HasPrefix(icon, "audio") {
		p.DeviceType = "audio"
	}
	p.Paired = boolProp(props, "Paired")
	p.Connected = boolProp(props, "Connected")
	p.Trusted = boolProp(props, "Trusted")

	return p
}

func stringProp(props map[string]dbus.Variant, key string) (string, bool) {
	if v, ok := props[key]; ok {
		s, ok := v.Value().(string)
		return s, ok
	}
	return "", false
}

func boolProp(props map[string]dbus.Variant, key string) bool {
	if v, ok := props[key]; ok {
		b, _ := v.Value().(bool)
		return b
	}
	return false
}

// Connect connects to a Bluetooth peripheral by address.
func (m *BlueZManager) Connect(ctx context.Context, address string, pair, trust bool) error {
	if trust {
		if out, err := exec.CommandContext(ctx, "bluetoothctl", "trust", address).CombinedOutput(); err != nil {
			m.logger.Warn("Failed to trust device", zap.Error(err), zap.String("output", string(out)))
		}
	}

	if pair {
		if out, err := exec.CommandContext(ctx, "bluetoothctl", "pair", address).CombinedOutput(); err != nil {
			return fmt.Errorf("pairing with %s: %w (output: %s)", address, err, string(out))
		}
	}

	out, err := exec.CommandContext(ctx, "bluetoothctl", "connect", address).CombinedOutput()
	if err != nil {
		return fmt.Errorf("connecting to %s: %w (output: %s)", address, err, string(out))
	}

	m.logger.Info("Connected to Bluetooth device", zap.String("address", address))
	return nil
}

// Disconnect disconnects from a Bluetooth peripheral.
func (m *BlueZManager) Disconnect(ctx context.Context, address string) error {
	out, err := exec.CommandContext(ctx, "bluetoothctl", "disconnect", address).CombinedOutput()
	if err != nil {
		return fmt.Errorf("disconnecting from %s: %w (output: %s)", address, err, string(out))
	}

	m.logger.Info("Disconnected from Bluetooth device", zap.String("address", address))
	return nil
}

// Forget removes a paired Bluetooth peripheral.
func (m *BlueZManager) Forget(ctx context.Context, address string) error {
	out, err := exec.CommandContext(ctx, "bluetoothctl", "remove", address).CombinedOutput()
	if err != nil {
		return fmt.Errorf("removing device %s: %w (output: %s)", address, err, string(out))
	}

	m.logger.Info("Forgot Bluetooth device", zap.String("address", address))
	return nil
}
