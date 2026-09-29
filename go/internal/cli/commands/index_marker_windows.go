//go:build windows

package commands

// markFATVolumesUnindexed is a no-op on Windows: writeConfigPartition has
// already taken the disk back offline, and bringing it online again for this
// would re-expose every partition to Explorer.
func markFATVolumesUnindexed(_ drive) error { return nil }
