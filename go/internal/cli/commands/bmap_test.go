package commands

import (
	"os"
	"testing"
)

func TestParseBmap(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.bmap")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	b, err := parseBmap(data)
	if err != nil {
		t.Fatalf("parseBmap: %v", err)
	}
	if b.BlockSize != 4096 {
		t.Errorf("BlockSize = %d, want 4096", b.BlockSize)
	}
	if b.ImageSize != 32768 {
		t.Errorf("ImageSize = %d, want 32768", b.ImageSize)
	}
	if len(b.Ranges) != 2 {
		t.Fatalf("len(Ranges) = %d, want 2", len(b.Ranges))
	}
	if b.Ranges[0].First != 0 || b.Ranges[0].Last != 1 {
		t.Errorf("Ranges[0] = %d-%d, want 0-1", b.Ranges[0].First, b.Ranges[0].Last)
	}
	if b.Ranges[1].First != 4 || b.Ranges[1].Last != 5 {
		t.Errorf("Ranges[1] = %d-%d, want 4-5", b.Ranges[1].First, b.Ranges[1].Last)
	}
}

func TestParseBmapSingleBlockRange(t *testing.T) {
	const x = `<bmap version="2.0"><ImageSize>4096</ImageSize><BlockSize>4096</BlockSize>` +
		`<ChecksumType>sha256</ChecksumType><BlockMap><Range chksum="ab">3</Range></BlockMap></bmap>`
	b, err := parseBmap([]byte(x))
	if err != nil {
		t.Fatalf("parseBmap: %v", err)
	}
	if len(b.Ranges) != 1 || b.Ranges[0].First != 3 || b.Ranges[0].Last != 3 {
		t.Fatalf("range = %+v, want single block 3", b.Ranges)
	}
}

func TestParseBmapRejectsNonSHA256(t *testing.T) {
	const x = `<bmap version="2.0"><ImageSize>1</ImageSize><BlockSize>1</BlockSize>` +
		`<ChecksumType>crc32</ChecksumType><BlockMap></BlockMap></bmap>`
	if _, err := parseBmap([]byte(x)); err == nil {
		t.Fatal("expected error for non-sha256 checksum type")
	}
}

func TestParseBmapRejectsGarbage(t *testing.T) {
	if _, err := parseBmap([]byte("not xml")); err == nil {
		t.Fatal("expected error for invalid XML")
	}
}
