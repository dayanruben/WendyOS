package commands

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// BmapRange is one contiguous run of mapped blocks in a bmap file.
// First and Last are inclusive block indices.
type BmapRange struct {
	First    int64
	Last     int64
	Checksum string // hex-encoded SHA256 of the range's bytes
}

// Bmap is the parsed subset of a bmaptool v2.0 block-map file that we need to
// reconstruct an image: the block size, total image size, and the mapped
// ranges with their per-range checksums.
type Bmap struct {
	BlockSize int64
	ImageSize int64
	Ranges    []BmapRange
}

// xmlBmap mirrors the on-disk bmaptool XML format for unmarshalling.
type xmlBmap struct {
	ImageSize    int64  `xml:"ImageSize"`
	BlockSize    int64  `xml:"BlockSize"`
	ChecksumType string `xml:"ChecksumType"`
	Ranges       []struct {
		Checksum string `xml:"chksum,attr"`
		Value    string `xml:",chardata"`
	} `xml:"BlockMap>Range"`
}

// parseBmap decodes a bmaptool v2.0 block-map file. It rejects checksum types
// other than sha256 (the only type the build emits) so callers fall back to dd
// rather than skipping verification.
func parseBmap(data []byte) (*Bmap, error) {
	var x xmlBmap
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parsing bmap: %w", err)
	}
	if x.BlockSize <= 0 || x.ImageSize <= 0 {
		return nil, fmt.Errorf("bmap: invalid block size %d or image size %d", x.BlockSize, x.ImageSize)
	}
	if t := strings.ToLower(strings.TrimSpace(x.ChecksumType)); t != "sha256" {
		return nil, fmt.Errorf("bmap: unsupported checksum type %q", x.ChecksumType)
	}

	b := &Bmap{BlockSize: x.BlockSize, ImageSize: x.ImageSize}
	for _, r := range x.Ranges {
		first, last, err := parseRange(strings.TrimSpace(r.Value))
		if err != nil {
			return nil, err
		}
		b.Ranges = append(b.Ranges, BmapRange{
			First:    first,
			Last:     last,
			Checksum: strings.TrimSpace(r.Checksum),
		})
	}
	return b, nil
}

// parseRange parses a bmap range token: either "N" or "N-M".
func parseRange(s string) (first, last int64, err error) {
	if s == "" {
		return 0, 0, fmt.Errorf("bmap: empty range")
	}
	dash := strings.IndexByte(s, '-')
	if dash < 0 {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("bmap: bad block number %q: %w", s, err)
		}
		return n, n, nil
	}
	first, err = strconv.ParseInt(s[:dash], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bmap: bad range start %q: %w", s, err)
	}
	last, err = strconv.ParseInt(s[dash+1:], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bmap: bad range end %q: %w", s, err)
	}
	if last < first {
		return 0, 0, fmt.Errorf("bmap: range end %d before start %d", last, first)
	}
	return first, last, nil
}
