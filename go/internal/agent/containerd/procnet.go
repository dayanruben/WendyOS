package containerd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// listeningPort is a single listening/bound socket parsed from /proc/<pid>/net/*.
type listeningPort struct {
	protocol string
	port     uint32
	address  string
}

// parseProcNet parses the contents of a /proc/<pid>/net/{tcp,tcp6,udp,udp6}
// file and returns the listening (TCP) or bound (UDP) sockets. protocol is the
// label to stamp on each entry ("tcp", "tcp6", "udp", "udp6"); ipv6 selects the
// address-decoding width.
//
// Selection rule: TCP sockets are included only in the LISTEN state (st 0A);
// UDP sockets are included when they have no connected peer (remote port 0),
// which is how a bound/serving UDP socket appears.
func parseProcNet(data []byte, protocol string, ipv6 bool) []listeningPort {
	isTCP := strings.HasPrefix(protocol, "tcp")
	var out []listeningPort

	sc := bufio.NewScanner(bytes.NewReader(data))
	first := true
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		// Header line ("sl local_address ...") and short lines are skipped.
		if first {
			first = false
			continue
		}
		if len(fields) < 4 {
			continue
		}
		local := fields[1]  // HEXADDR:HEXPORT
		remote := fields[2] // HEXADDR:HEXPORT
		st := fields[3]

		if isTCP {
			if st != "0A" { // TCP_LISTEN
				continue
			}
		} else {
			// UDP: include only unconnected (bound) sockets — remote port 0.
			if _, rport, ok := splitHexAddr(remote); !ok || rport != 0 {
				continue
			}
		}

		addrHex, port, ok := splitHexAddr(local)
		if !ok {
			continue
		}
		addr := decodeHexAddr(addrHex, ipv6)
		if addr == "" {
			continue
		}
		out = append(out, listeningPort{protocol: protocol, port: port, address: addr})
	}
	return out
}

// splitHexAddr splits a "HEXADDR:HEXPORT" token into the address hex and the
// decoded port.
func splitHexAddr(tok string) (addrHex string, port uint32, ok bool) {
	i := strings.LastIndex(tok, ":")
	if i < 0 {
		return "", 0, false
	}
	p, err := strconv.ParseUint(tok[i+1:], 16, 32)
	if err != nil {
		return "", 0, false
	}
	return tok[:i], uint32(p), true
}

// decodeHexAddr decodes the hex local address from /proc/net/* into a printable
// IP. IPv4 is 8 hex chars in little-endian byte order; IPv6 is 32 hex chars
// stored as four little-endian 32-bit words.
func decodeHexAddr(h string, ipv6 bool) string {
	if !ipv6 {
		if len(h) != 8 {
			return ""
		}
		b := make([]byte, 4)
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
			if err != nil {
				return ""
			}
			b[3-i] = byte(v) // little-endian → reverse
		}
		return net.IPv4(b[0], b[1], b[2], b[3]).String()
	}

	if len(h) != 32 {
		return ""
	}
	ip := make([]byte, 16)
	// Four 32-bit words, each little-endian.
	for w := 0; w < 4; w++ {
		word := h[8*w : 8*w+8]
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(word[2*i:2*i+2], 16, 8)
			if err != nil {
				return ""
			}
			ip[w*4+(3-i)] = byte(v)
		}
	}
	return net.IP(ip).String()
}

// GetListeningPorts returns the listening TCP and bound UDP sockets for every
// container belonging to appName, read from each container's network namespace
// via /proc/<pid>/net/*. Results are de-duplicated and sorted by port.
func (c *Client) GetListeningPorts(ctx context.Context, appName string) ([]*agentpb.PortEntry, error) {
	ids, err := c.ContainerIDsForApp(ctx, appName)
	if err != nil || len(ids) == 0 {
		// Fall back to treating appName as a bare container ID.
		ids = []string{appName}
	}

	nsCtx := c.withNamespace(ctx)
	seen := make(map[string]bool)
	var out []*agentpb.PortEntry

	protocols := []struct {
		name string
		ipv6 bool
	}{
		{"tcp", false}, {"tcp6", true}, {"udp", false}, {"udp6", true},
	}

	for _, id := range ids {
		container, err := c.client.LoadContainer(nsCtx, id)
		if err != nil {
			continue
		}
		task, err := container.Task(nsCtx, nil)
		if err != nil {
			continue
		}
		pid := task.Pid()
		for _, p := range protocols {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/net/%s", pid, p.name))
			if err != nil {
				continue
			}
			for _, lp := range parseProcNet(data, p.name, p.ipv6) {
				key := fmt.Sprintf("%s|%s|%d", lp.protocol, lp.address, lp.port)
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, &agentpb.PortEntry{
					Protocol: lp.protocol,
					Port:     lp.port,
					Address:  lp.address,
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Protocol < out[j].Protocol
	})
	return out, nil
}
