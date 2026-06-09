//go:build darwin

package commands

import (
	"slices"
	"testing"
)

func TestDarwinDDArgsUseFullblockForStreamedInput(t *testing.T) {
	args := darwinDDArgs("/dev/rdisk61", "64m")

	if !slices.Contains(args, "iflag=fullblock") {
		t.Fatalf("darwinDDArgs() = %v; want iflag=fullblock", args)
	}
	if slices.Contains(args, "conv=sync") {
		t.Fatalf("darwinDDArgs() = %v; conv=sync pads every short pipe read", args)
	}
}
