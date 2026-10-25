package network

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeNMCLI stands in for nmcli on a connected device. Its state lives in
// files in one directory (the format argument):
//
//   - current: NetworkManager's list, printed by every `device wifi list`.
//   - air: what a scan finds. A scan copies it to current.
//   - busy: while it exists, the driver refuses scans. A refused scan ends at
//     once and leaves current as it was.
//   - hang: while it exists, a scan never ends.
//   - status: printed by `device status`.
//   - requests: one line per scan request, "rescan" for `device wifi
//     rescan` and "scan" for `device wifi list --rescan yes`.
//
// Like the real tool, `device wifi rescan` returns as soon as the scan is
// requested, and a plain `device wifi list` prints the old list at once.
const fakeNMCLI = `#!/bin/sh
cd %q || exit 1
case "$*" in
*"device status"*)
	cat status
	;;
*"device wifi rescan"*)
	echo rescan >> requests
	;;
*"device wifi list --rescan yes"*)
	echo scan >> requests
	if [ -e hang ]; then
		exec sleep 10
	fi
	if [ ! -e busy ]; then
		sleep 0.2
		cp air current
	fi
	cat current
	;;
*"device wifi list"*)
	cat current
	;;
esac
`

// oldList is NetworkManager's list on a connected device that has not
// scanned for a while: little more than the current network.
const oldList = `*:3-UTUM-IOT:62:WPA2
:UTUMadv:42:WPA3
`

// scanList is what a full scan finds.
const scanList = `*:3-UTUM-IOT:62:WPA2
:Christos iPhone:87:WPA2
:UTUMadv:42:WPA3
:3-UTUM-IOT:55:WPA2
:Guest:40:WPA2
`

var (
	oldSSIDs  = []string{"3-UTUM-IOT", "UTUMadv"}
	scanSSIDs = []string{"3-UTUM-IOT", "Christos iPhone", "Guest", "UTUMadv"}
)

type fakeWiFi struct {
	t       *testing.T
	dir     string
	manager *NMCLINetworkManager
}

// newFakeWiFi returns a manager running fakeNMCLI on a connected device whose
// list holds oldList and whose scans find scanList.
func newFakeWiFi(t *testing.T) *fakeWiFi {
	t.Helper()
	f := &fakeWiFi{t: t, dir: t.TempDir()}
	f.write("nmcli", fmt.Sprintf(fakeNMCLI, f.dir))
	f.write("current", oldList)
	f.write("air", scanList)
	f.write("status", "ethernet:connected\nwifi:connected\nloopback:connected (externally)\n")
	f.manager = &NMCLINetworkManager{logger: zap.NewNop(), nmcliPath: filepath.Join(f.dir, "nmcli")}
	return f
}

func (f *fakeWiFi) write(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0700); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeWiFi) remove(name string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.dir, name)); err != nil {
		f.t.Fatal(err)
	}
}

// requests returns the scan requests made so far.
func (f *fakeWiFi) requests() []string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, "requests"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func (f *fakeWiFi) requireRequests(want ...string) {
	f.t.Helper()
	if got := f.requests(); !slices.Equal(got, want) {
		f.t.Fatalf("scan requests = %q; want %q", got, want)
	}
}

// list calls ListWiFiNetworks and returns the sorted SSIDs.
func (f *fakeWiFi) list() []string {
	f.t.Helper()
	ssids, err := listSSIDs(f.manager)
	if err != nil {
		f.t.Fatal(err)
	}
	return ssids
}

func (f *fakeWiFi) requireList(want []string) {
	f.t.Helper()
	if got := f.list(); !slices.Equal(got, want) {
		f.t.Fatalf("networks = %q; want %q", got, want)
	}
}

// waitOutInterval makes the last scan the agent asked for an interval old.
func (f *fakeWiFi) waitOutInterval() {
	f.manager.lastScan = time.Now().Add(-scanInterval)
}

func listSSIDs(manager *NMCLINetworkManager) ([]string, error) {
	nets, err := manager.ListWiFiNetworks(context.Background())
	if err != nil {
		return nil, err
	}
	var ssids []string
	for _, n := range nets {
		ssids = append(ssids, n.GetSsid())
	}
	slices.Sort(ssids)
	return ssids, nil
}

// TestListWiFiNetworksWaitsForScan: the first list must come from the scan
// it asked for, not from NetworkManager's old list.
func TestListWiFiNetworksWaitsForScan(t *testing.T) {
	f := newFakeWiFi(t)
	nets, err := f.manager.ListWiFiNetworks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.requireRequests("scan")

	hotspot := bySSID(nets, "Christos iPhone")
	if hotspot == nil {
		t.Fatalf("Christos iPhone missing from %d networks", len(nets))
	}
	if hotspot.GetSignalStrength() != 87 {
		t.Errorf("Christos iPhone signal = %d; want 87", hotspot.GetSignalStrength())
	}
	if cur := bySSID(nets, "3-UTUM-IOT"); cur == nil || !cur.GetIsConnected() {
		t.Errorf("3-UTUM-IOT should be listed as connected")
	}
	if len(nets) != len(scanSSIDs) {
		t.Errorf("got %d networks; want %d", len(nets), len(scanSSIDs))
	}
}

// TestListWiFiNetworksScansOncePerInterval: the WiFi screen polls every
// 2.5 s. Calls inside the interval get NetworkManager's list without a scan.
func TestListWiFiNetworksScansOncePerInterval(t *testing.T) {
	f := newFakeWiFi(t)
	for i := 0; i < 5; i++ {
		f.requireList(scanSSIDs)
	}
	f.requireRequests("scan")
}

// TestListWiFiNetworksSharesScanAcrossCallers: callers that arrive together
// (the WiFi screen, BLE, MCP) share one scan, and those that waited on it
// get its results.
func TestListWiFiNetworksSharesScanAcrossCallers(t *testing.T) {
	f := newFakeWiFi(t)
	const callers = 8
	results := make([][]string, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = listSSIDs(f.manager)
		}()
	}
	close(start)
	wg.Wait()

	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if !slices.Equal(results[i], scanSSIDs) {
			t.Errorf("caller %d got %q; want %q", i, results[i], scanSSIDs)
		}
	}
	f.requireRequests("scan")
}

// TestListWiFiNetworksDoesNotRetryRefusedScan: a scan the driver refused is
// not followed by another one. The next scan waits for the next interval,
// even if the link is quiet by then.
func TestListWiFiNetworksDoesNotRetryRefusedScan(t *testing.T) {
	f := newFakeWiFi(t)
	f.write("busy", "")
	f.requireList(oldSSIDs)
	f.requireRequests("scan")

	f.remove("busy")
	f.requireList(oldSSIDs)
	f.requireRequests("scan")

	f.waitOutInterval()
	f.requireList(scanSSIDs)
	f.requireRequests("scan", "scan")
}

// TestListWiFiNetworksScansAgainAfterQuietPeriod: after an interval without
// list calls, the next one shows a new scan, not the list left by the last.
func TestListWiFiNetworksScansAgainAfterQuietPeriod(t *testing.T) {
	f := newFakeWiFi(t)
	f.requireList(scanSSIDs)

	// The hotspot goes away and a neighbor's network appears.
	f.write("air", "*:3-UTUM-IOT:60:WPA2\n:UTUMadv:45:WPA3\n:Neighbor:30:WPA2\n")
	f.manager.lastScan = time.Now().Add(-5 * time.Minute)
	f.requireList([]string{"3-UTUM-IOT", "Neighbor", "UTUMadv"})
	f.requireRequests("scan", "scan")
}

// TestListWiFiNetworksBoundsScanWait: a scan that never ends doesn't hold
// the call. It returns NetworkManager's list once scanTimeout passes.
func TestListWiFiNetworksBoundsScanWait(t *testing.T) {
	old := scanTimeout
	scanTimeout = 300 * time.Millisecond
	t.Cleanup(func() { scanTimeout = old })

	f := newFakeWiFi(t)
	f.write("hang", "")
	start := time.Now()
	f.requireList(oldSSIDs)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("list took %v; want about scanTimeout (%v)", elapsed, scanTimeout)
	}
	f.requireRequests("scan")

	// The timed-out scan counts: the next call doesn't scan.
	f.remove("hang")
	f.requireList(oldSSIDs)
	f.requireRequests("scan")
}

// TestListWiFiNetworksSkipsScanWhileConnecting: no scan is asked for while
// a WiFi connection is being activated, nor right after it comes up.
func TestListWiFiNetworksSkipsScanWhileConnecting(t *testing.T) {
	f := newFakeWiFi(t)
	f.write("status", "ethernet:connected\nwifi:connecting (configuring)\n")
	f.requireList(oldSSIDs)
	f.requireRequests()

	f.write("status", "ethernet:connected\nwifi:connected\n")
	f.requireList(oldSSIDs)
	f.requireRequests()

	f.waitOutInterval()
	f.requireList(scanSSIDs)
	f.requireRequests("scan")
}
