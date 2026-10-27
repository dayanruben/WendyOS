package network

import (
	"context"
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	agentpb "github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

const listUUIDs = "-t -f UUID,TYPE connection show"

// callsWithPrefix returns the logged calls that start with prefix.
func callsWithPrefix(calls []string, prefix string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

const newUUID = "0f9e8d7c-6b5a-4c3d-2e1f-001122334455"

// addOK is nmcli's answer to a successful `connection add`.
var addOK = reply{args: "connection add ", prefix: true,
	out: "Connection 'Hidden' (" + newUUID + ") successfully added.\n"}

func connect(t *testing.T, path string, req *agentpb.ConnectToWiFiRequest) error {
	t.Helper()
	n := &NMCLINetworkManager{logger: zap.NewNop(), nmcliPath: path}
	return n.ConnectToWiFi(context.Background(), req)
}

func TestAddedUUIDRe(t *testing.T) {
	cases := map[string]string{
		"Connection 'Home' (" + newUUID + ") successfully added.\n":                                                        newUUID,
		"Connection 'a (b) c' (" + newUUID + ") successfully added.\n":                                                     newUUID,
		"Connection 'x (11111111-2222-3333-4444-555555555555) successfully added' (" + newUUID + ") successfully added.\n": newUUID,
		"Warning: something.\n": "",
	}
	for out, want := range cases {
		got := ""
		if m := addedUUIDRe.FindAllStringSubmatch(out, -1); len(m) > 0 {
			got = m[len(m)-1][1]
		}
		if got != want {
			t.Errorf("uuid in %q = %q; want %q", out, got, want)
		}
	}
}

func TestConnectNewProfileLooksUpOnce(t *testing.T) {
	path, log := writeFakeNMCLI(t, append(severalProfiles(),
		addOK,
		reply{args: "connection up ", prefix: true},
	)...)
	if err := connect(t, path, &agentpb.ConnectToWiFiRequest{Ssid: "Hidden", Password: "secret123", Hidden: proto.Bool(true)}); err != nil {
		t.Fatal(err)
	}
	calls := readCalls(t, log)
	if got := len(callsWithPrefix(calls, listUUIDs)); got != 1 {
		t.Errorf("profiles listed %d times; want 1:\n%s", got, strings.Join(calls, "\n"))
	}
	adds := callsWithPrefix(calls, "connection add ")
	if len(adds) != 1 || strings.Contains(adds[0], "connection.uuid") {
		t.Fatalf("adds %q; want one, without connection.uuid", adds)
	}
	if ups := callsWithPrefix(calls, "connection up "); len(ups) != 1 || ups[0] != "connection up "+newUUID {
		t.Errorf("activated %q; want the added profile", ups)
	}
}

func TestConnectFailedNewProfileIsDeleted(t *testing.T) {
	path, log := writeFakeNMCLI(t, append(severalProfiles(),
		addOK,
		reply{args: "connection up ", prefix: true, out: "Error: Secrets were required.\n", code: 4},
		reply{args: "connection delete ", prefix: true},
	)...)
	if err := connect(t, path, &agentpb.ConnectToWiFiRequest{Ssid: "Hidden", Password: "wrong-pass", Hidden: proto.Bool(true)}); err == nil {
		t.Fatal("want the activation error")
	}
	calls := readCalls(t, log)
	if dels := callsWithPrefix(calls, "connection delete "); len(dels) != 1 || dels[0] != "connection delete "+newUUID {
		t.Errorf("deleted %q; want only the profile this attempt added", dels)
	}
	if got := len(callsWithPrefix(calls, listUUIDs)); got != 1 {
		t.Errorf("profiles listed %d times; want 1", got)
	}
}

// Without a UUID in nmcli's answer, the added profile is found by its SSID.
func TestConnectFindsAddedProfileWithoutUUIDLine(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nmcli"
	script := `#!/bin/sh
d=$(dirname "$0")
case "$*" in
"` + listUUIDs + `") [ -f "$d/added" ] && echo "uuid-x:802-11-wireless"; exit 0 ;;
"` + batchArgs("uuid-x") + `") printf '%s' '` + batchRecord("uuid-x", "wpa-psk", "", "Hidden") + `'; exit 0 ;;
"connection add "*) touch "$d/added"; exit 0 ;;
"connection up uuid-x") exit 0 ;;
esac
echo "unexpected nmcli call: $*" >&2
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := connect(t, path, &agentpb.ConnectToWiFiRequest{Ssid: "Hidden", Password: "secret123", Hidden: proto.Bool(true)}); err != nil {
		t.Fatal(err)
	}
}

func TestConnectFailedExistingProfileIsKept(t *testing.T) {
	path, log := writeFakeNMCLI(t, append(severalProfiles(),
		reply{args: "connection modify uuid-c ", prefix: true},
		reply{args: "connection up uuid-c", out: "Error: Secrets were required.\n", code: 4},
	)...)
	if err := connect(t, path, &agentpb.ConnectToWiFiRequest{Ssid: "Office", Password: "wrong-pass", Hidden: proto.Bool(true)}); err == nil {
		t.Fatal("want the activation error")
	}
	calls := readCalls(t, log)
	if dels := callsWithPrefix(calls, "connection delete "); len(dels) != 0 {
		t.Errorf("deleted %q; a profile that existed before must be kept", dels)
	}
	if adds := callsWithPrefix(calls, "connection add "); len(adds) != 0 {
		t.Errorf("added %q; the existing profile must be reused", adds)
	}
}
