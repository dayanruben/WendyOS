package containerd

import "testing"

func TestParseProcNetTCPListening(t *testing.T) {
	// Header + a LISTEN socket on 0.0.0.0:8080 (0050 hex port? no: 8080 = 0x1F90)
	// and an ESTABLISHED socket (st 01) that must be excluded.
	data := []byte(
		"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
			"   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1\n" +
			"   1: 0100007F:1538 0100007F:C001 01 00000000:00000000 00:00000000 00000000  1000        0 2\n",
	)
	got := parseProcNet(data, "tcp", false)
	if len(got) != 1 {
		t.Fatalf("got %d ports, want 1 (only LISTEN): %+v", len(got), got)
	}
	if got[0].port != 8080 {
		t.Errorf("port = %d, want 8080", got[0].port)
	}
	if got[0].address != "0.0.0.0" {
		t.Errorf("address = %q, want 0.0.0.0", got[0].address)
	}
	if got[0].protocol != "tcp" {
		t.Errorf("protocol = %q, want tcp", got[0].protocol)
	}
}

func TestParseProcNetLocalhostV4(t *testing.T) {
	// 127.0.0.1:5432 (1538 = 0x1538 = 5432) LISTEN.
	data := []byte(
		"  sl  local_address rem_address   st\n" +
			"   0: 0100007F:1538 00000000:0000 0A\n",
	)
	got := parseProcNet(data, "tcp", false)
	if len(got) != 1 || got[0].address != "127.0.0.1" || got[0].port != 5432 {
		t.Fatalf("got %+v, want 127.0.0.1:5432", got)
	}
}

func TestParseProcNetUDPBoundOnly(t *testing.T) {
	// A bound UDP socket (rem port 0) is included; a connected one is excluded.
	data := []byte(
		"  sl  local_address rem_address   st\n" +
			"   0: 00000000:14E9 00000000:0000 07\n" + // bound :5353
			"   1: 0100007F:0035 0100007F:1234 07\n", // connected → excluded
	)
	got := parseProcNet(data, "udp", false)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1 bound udp: %+v", len(got), got)
	}
	if got[0].port != 5353 {
		t.Errorf("port = %d, want 5353", got[0].port)
	}
}

func TestParseProcNetTCP6Wildcard(t *testing.T) {
	// IPv6 wildcard [::]:443 (01BB) LISTEN. 32 zero hex chars → "::".
	data := []byte(
		"  sl  local_address rem_address   st\n" +
			"   0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A\n",
	)
	got := parseProcNet(data, "tcp6", true)
	if len(got) != 1 || got[0].address != "::" || got[0].port != 443 {
		t.Fatalf("got %+v, want [::]:443", got)
	}
}

func TestDecodeHexAddrV6Loopback(t *testing.T) {
	// ::1 is stored as 00000000000000000000000001000000 (last word little-endian).
	if got := decodeHexAddr("00000000000000000000000001000000", true); got != "::1" {
		t.Errorf("decodeHexAddr loopback = %q, want ::1", got)
	}
}
