//go:build windows

package winusb

// Minimal ADB wire protocol over the WinUSB transport — the Windows counterpart
// of the gousb-based adb package. Enough to drive NVIDIA's initrd-flash gadget:
// the CNXN handshake, shell-protocol-v2 command execution, and sync (file push)
// used by stage-2 flashing. adbd on the flashing initrd runs insecure (no AUTH).

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// ADB message commands (little-endian 4-byte tags).
const (
	adbCNXN = 0x4e584e43
	adbAUTH = 0x48545541
	adbOPEN = 0x4e45504f
	adbOKAY = 0x59414b4f
	adbCLSE = 0x45534c43
	adbWRTE = 0x45545257

	adbVersion    = 0x01000001
	adbMaxPayload = 256 * 1024
)

// shell-protocol-v2 stream ids.
const (
	shellV2Stdout = 1
	shellV2Stderr = 2
	shellV2Exit   = 3
)

// ADB is an ADB session over a WinUSB device.
type ADB struct {
	dev    *USBDevice
	nextID uint32
	Banner string
	rbuf   []byte // buffered bulk-IN bytes read past a message boundary, not yet consumed
}

// ExitError reports a non-zero shell exit status.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("shell exited with status %d", e.Code) }

// NewADB performs the CNXN handshake over an already-open WinUSB device.
func NewADB(dev *USBDevice) (*ADB, error) {
	a := &ADB{dev: dev, nextID: 1}
	if err := a.connect(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *ADB) connect() error {
	if err := a.writeMsg(adbCNXN, adbVersion, adbMaxPayload, []byte("host::\x00")); err != nil {
		return fmt.Errorf("sending CNXN: %w", err)
	}
	for {
		cmd, _, _, data, err := a.readMsg()
		if err != nil {
			return fmt.Errorf("reading CNXN reply: %w", err)
		}
		switch cmd {
		case adbCNXN:
			a.Banner = string(data)
			// The engine's error model depends on Shell reporting non-zero exits.
			// Only shell-protocol-v2 carries an exit status; the legacy shell: service
			// does not, so a failed command there would look like success. The T264
			// flashing gadget always advertises shell_v2 (and its legacy shell:
			// returns "fork failed"), so require it rather than silently run blind.
			if !strings.Contains(a.Banner, "shell_v2") {
				return fmt.Errorf("device adbd does not advertise shell_v2 (banner %q); required for reliable exit-status reporting", a.Banner)
			}
			return nil
		case adbAUTH:
			return fmt.Errorf("device requires ADB authentication (secure adbd); not supported")
		}
	}
}

// Shell runs a command and returns combined stdout+stderr; a non-zero exit yields
// an *ExitError. Uses shell-protocol-v2 (required by connect), which frames an
// exit status the engine relies on.
func (a *ADB) Shell(command string) (string, error) {
	return a.shellV2Run("shell,v2,raw:" + command)
}

func (a *ADB) shellV2Run(service string) (string, error) {
	local := a.nextID
	a.nextID++
	if err := a.writeMsg(adbOPEN, local, 0, append([]byte(service), 0)); err != nil {
		return "", err
	}
	var out, buf []byte
	exit := -1
	for {
		for len(buf) >= 5 {
			length := int(binary.LittleEndian.Uint32(buf[1:5]))
			if len(buf) < 5+length {
				break
			}
			id, payload := buf[0], buf[5:5+length]
			switch id {
			case shellV2Stdout, shellV2Stderr:
				out = append(out, payload...)
			case shellV2Exit:
				if length >= 1 {
					exit = int(payload[0])
				}
			}
			buf = buf[5+length:]
		}
		cmd, a0, a1, data, err := a.readMsg()
		if err != nil {
			return string(out), err
		}
		if a1 != local {
			continue // stale message addressed to a previous stream
		}
		switch cmd {
		case adbWRTE:
			buf = append(buf, data...)
			a.writeMsg(adbOKAY, local, a0, nil)
		case adbCLSE:
			a.writeMsg(adbCLSE, local, a0, nil)
			switch {
			case exit > 0:
				return string(out), &ExitError{Code: exit}
			case exit < 0:
				// Stream closed without an exit-status packet: abnormal for shell_v2
				// (a killed/OOM'd command, or a truncated stream). Do not report success.
				return string(out), fmt.Errorf("shell stream closed without an exit status")
			default:
				return string(out), nil
			}
		}
	}
}

// Push streams r to remotePath via the sync SEND service with the given unix mode.
func (a *ADB) Push(r io.Reader, remotePath string, mode int) error {
	local := a.nextID
	a.nextID++
	if err := a.writeMsg(adbOPEN, local, 0, []byte("sync:\x00")); err != nil {
		return err
	}
	remote, err := a.expectOkay(local)
	if err != nil {
		return fmt.Errorf("opening sync stream: %w", err)
	}
	send := func(p []byte) error {
		if err := a.writeMsg(adbWRTE, local, remote, p); err != nil {
			return err
		}
		for {
			cmd, a0, a1, _, err := a.readMsg()
			if err != nil {
				return err
			}
			if a1 != local {
				continue // stale message addressed to a previous stream
			}
			switch cmd {
			case adbOKAY:
				return nil
			case adbWRTE:
				a.writeMsg(adbOKAY, local, a0, nil)
			case adbCLSE:
				return fmt.Errorf("sync stream closed unexpectedly")
			}
		}
	}
	if err := send(syncReq("SEND", []byte(fmt.Sprintf("%s,%d", remotePath, mode)))); err != nil {
		return fmt.Errorf("SEND: %w", err)
	}
	bufc := make([]byte, 64*1024)
	for {
		n, rerr := r.Read(bufc)
		if n > 0 {
			if err := send(syncReq("DATA", bufc[:n])); err != nil {
				return fmt.Errorf("DATA: %w", err)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("reading push source: %w", rerr)
		}
	}
	done := make([]byte, 8)
	copy(done, "DONE")
	if err := send(done); err != nil {
		return fmt.Errorf("DONE: %w", err)
	}
	for {
		cmd, a0, a1, pd, err := a.readMsg()
		if err != nil {
			return fmt.Errorf("reading sync result: %w", err)
		}
		if a1 != local {
			continue // stale message addressed to a previous stream
		}
		if cmd == adbWRTE {
			a.writeMsg(adbOKAY, local, a0, nil)
			if len(pd) >= 4 && string(pd[0:4]) == "FAIL" {
				msg := ""
				if len(pd) > 8 {
					msg = string(pd[8:])
				}
				return fmt.Errorf("push rejected: %s", msg)
			}
		}
		break
	}
	a.writeMsg(adbCLSE, local, remote, nil)
	return nil
}

func (a *ADB) expectOkay(local uint32) (uint32, error) {
	for {
		cmd, a0, a1, _, err := a.readMsg()
		if err != nil {
			return 0, err
		}
		if a1 != local {
			continue
		}
		switch cmd {
		case adbOKAY:
			return a0, nil
		case adbCLSE:
			return 0, fmt.Errorf("stream rejected by device")
		}
	}
}

func syncReq(id string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	copy(b[0:], id)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(payload)))
	copy(b[8:], payload)
	return b
}

// writeMsg sends one ADB message: a 24-byte header then the payload.
func (a *ADB) writeMsg(cmd, arg0, arg1 uint32, data []byte) error {
	var check uint32
	for _, b := range data {
		check += uint32(b)
	}
	hdr := make([]byte, 24)
	binary.LittleEndian.PutUint32(hdr[0:], cmd)
	binary.LittleEndian.PutUint32(hdr[4:], arg0)
	binary.LittleEndian.PutUint32(hdr[8:], arg1)
	binary.LittleEndian.PutUint32(hdr[12:], uint32(len(data)))
	binary.LittleEndian.PutUint32(hdr[16:], check)
	binary.LittleEndian.PutUint32(hdr[20:], cmd^0xffffffff)
	if _, err := a.dev.WriteBulk(hdr); err != nil {
		return err
	}
	if len(data) > 0 {
		// Length-prefixed: write the payload with no end-of-transfer ZLP (WriteImage
		// would append one for exact-multiple lengths, which adbd reads as a spurious
		// next-message boundary and desyncs the stream).
		if err := a.dev.writeBulkChunked(data); err != nil {
			return err
		}
	}
	return nil
}

// readMsg reads one ADB message: a 24-byte header then data_length payload bytes.
// WinUSB's buffered ReadPipe can coalesce several adbd messages into one transfer,
// so any bytes read past this message's end are retained in a.rbuf for the next
// call rather than discarded (dropping them would desync the stream).
func (a *ADB) readMsg() (cmd, arg0, arg1 uint32, data []byte, err error) {
	if err = a.fill(24); err != nil {
		return 0, 0, 0, nil, err
	}
	hdr := a.rbuf[:24]
	cmd = binary.LittleEndian.Uint32(hdr[0:])
	arg0 = binary.LittleEndian.Uint32(hdr[4:])
	arg1 = binary.LittleEndian.Uint32(hdr[8:])
	dlen := binary.LittleEndian.Uint32(hdr[12:])
	if dlen > adbMaxPayload {
		return 0, 0, 0, nil, fmt.Errorf("ADB payload too large: %d", dlen)
	}
	if err = a.fill(24 + int(dlen)); err != nil {
		return 0, 0, 0, nil, err
	}
	data = append([]byte(nil), a.rbuf[24:24+dlen]...)
	a.rbuf = a.rbuf[24+dlen:]
	if len(a.rbuf) == 0 {
		a.rbuf = nil // release the backing array once fully consumed
	}
	return cmd, arg0, arg1, data, nil
}

// fill ensures a.rbuf holds at least n bytes, reading more from the bulk IN pipe.
func (a *ADB) fill(n int) error {
	for len(a.rbuf) < n {
		buf := make([]byte, 64*1024)
		m, err := a.dev.ReadBulk(buf)
		if err != nil {
			return err
		}
		if m == 0 {
			return io.ErrUnexpectedEOF
		}
		a.rbuf = append(a.rbuf, buf[:m]...)
	}
	return nil
}
