package commands

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// spotlightMarker at a volume root stops macOS Spotlight indexing or searching
// it. Indexing a re-inserted card's FAT volumes can wedge the macOS 26 FAT
// driver and hang the next flash, so every FAT volume we write carries one.
const spotlightMarker = ".metadata_never_index"

// markerTimeout bounds each step of marking a volume.
const markerTimeout = 10 * time.Second

// markFATVolumesDeadline bounds the whole marker step. Mounting and probing a
// volume involve I/O no subprocess timeout covers, so past it the step is
// abandoned rather than allowed to hold up the eject.
var markFATVolumesDeadline = time.Minute

// markFATVolumes marks every FAT volume on d. A failure only costs the
// protection on the next re-flash, so it is reported, never fatal.
func markFATVolumes(d drive) {
	done := make(chan error, 1)
	go func() { done <- markFATVolumesUnindexed(d) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(markFATVolumesDeadline):
		err = fmt.Errorf("gave up after %s", markFATVolumesDeadline)
	}
	if err != nil {
		fmt.Printf("Note: could not mark %s's FAT volumes to skip Spotlight indexing: %v\n", d.DevicePath, err)
	}
}

// parseDiskutilSlices returns the slices of diskDev listed by
// `diskutil list diskDev`, as /dev nodes.
func parseDiskutilSlices(out, diskDev string) []string {
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(strings.TrimPrefix(diskDev, "/dev/")) + `s[0-9]+$`)
	var slices []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && re.MatchString(fields[len(fields)-1]) {
			slices = append(slices, "/dev/"+fields[len(fields)-1])
		}
	}
	return slices
}

// parseBundleType returns the "Type (Bundle)" of `diskutil info` output: the
// filesystem, e.g. "msdos" for FAT.
func parseBundleType(info string) string {
	for _, line := range strings.Split(info, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Type (Bundle):"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseLsblkFAT returns the vfat partitions in `lsblk -o NAME,FSTYPE -n -r`
// output, as /dev nodes.
func parseLsblkFAT(out string) []string {
	var parts []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "vfat" {
			parts = append(parts, "/dev/"+fields[0])
		}
	}
	return parts
}
