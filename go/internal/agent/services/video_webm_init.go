package services

// webmInitialization retains only the EBML and Segment metadata preceding the
// first Cluster. A model joining an existing producer needs these bytes to
// initialize its demuxer. Payload bytes and sample identities stay unchanged.
// The hub mutex protects this state. See the WebM container guidelines:
// https://www.webmproject.org/docs/container/
type webmInitialization struct {
	pending []byte
	header  []byte
	offset  int
	segment bool
	failed  bool
}

const maxWebMInitialization = 1 << 20

func (w *webmInitialization) feed(data []byte) []byte {
	if w.header != nil || w.failed {
		return w.header
	}
	// Only retain a bounded prefix, even if the first read contains a large Cluster.
	remaining := maxWebMInitialization - len(w.pending)
	if len(data) > remaining {
		data = data[:remaining]
	}
	w.pending = append(w.pending, data...)
	for {
		id, n, _, ok := webmVint(w.pending[w.offset:], true)
		if !ok {
			break
		}
		if w.offset == 0 && id != 0x1a45dfa3 {
			w.failed = true
			break
		}
		if w.segment && id == 0x1f43b675 {
			w.header = append([]byte(nil), w.pending[:w.offset]...)
			w.pending = nil
			return w.header
		}
		size, m, unknown, ok := webmVint(w.pending[w.offset+n:], false)
		if !ok {
			break
		}
		end := w.offset + n + m
		if id == 0x18538067 && !w.segment {
			// A resumed stream has a different byte length; declare an unknown-sized
			// Segment, preserving the encoded size's width.
			w.pending[w.offset+n] = byte((1 << (9 - m)) - 1)
			for i := w.offset + n + 1; i < end; i++ {
				w.pending[i] = 0xff
			}
			w.segment = true
			w.offset = end
			continue
		}
		if unknown || size > uint64(maxWebMInitialization-end) {
			w.failed = true
			break
		}
		if size > uint64(len(w.pending)-end) {
			break
		}
		w.offset = end + int(size)
	}
	if len(w.pending) == maxWebMInitialization {
		w.failed = true
	}
	if w.failed {
		w.pending = nil
	}
	return nil
}

// webmVint reads an EBML identifier or data size without allocating. Identifier
// marker bits remain part of the value; size marker bits do not.
func webmVint(b []byte, id bool) (value uint64, width int, unknown, ok bool) {
	if len(b) == 0 || b[0] == 0 {
		return
	}
	marker := byte(0x80)
	width = 1
	for b[0]&marker == 0 {
		marker >>= 1
		width++
	}
	if (id && width > 4) || len(b) < width {
		return 0, 0, false, false
	}
	value = uint64(b[0])
	if !id {
		value &= uint64(marker - 1)
	}
	for _, v := range b[1:width] {
		value = value<<8 | uint64(v)
	}
	unknown = !id && value == (uint64(1)<<(7*width))-1
	return value, width, unknown, true
}
