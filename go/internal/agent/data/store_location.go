package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Where the episode store lives when WENDY_DATA_DIR does not say.
//
// The store used to default to LegacyRoot, on the root filesystem. On WendyOS
// that is one A/B slot of 4 to 12 GiB, so with DefaultReserveBytes of headroom
// to keep, a slot with less than 5 GiB free refused every recording while the
// /data partition beside it sat almost empty. WendyOS already bind-mounts
// /var/lib/wendy from /data/wendy (var-lib-wendy.mount in WendyOS-Builder) for
// app volumes, so the store follows them there: DataDiskRoot.
//
// The folder is wendy-data rather than data because /var/lib/wendy already
// holds app-data (the per-app Wendy Data socket directories), and a bare data
// beside it, on a disk mounted at /data, would say nothing about what it holds.
// Its episodes and inference children keep the shape of the old
// /var/lib/wendy-agent/data.
const (
	// DataDiskRoot is the default store on a device whose /var/lib/wendy is a
	// separate filesystem larger than the root one, which is every WendyOS
	// board with a /data partition.
	DataDiskRoot = "/var/lib/wendy/wendy-data/episodes"
	// LegacyRoot is the store every earlier agent defaulted to, and the one
	// this agent still uses where DataDiskRoot would gain nothing: a host
	// without /var/lib/wendy, or one where it shares the root filesystem or is
	// smaller than it.
	LegacyRoot = "/var/lib/wendy-agent/data/episodes"
)

// StoreLocation says where the episode store is and why it is there.
type StoreLocation struct {
	// Path is the episode store.
	Path string
	// CampaignDir holds deployed campaign plans. It is beside Path except when
	// the store moved to DataDiskRoot by default: plans are a few KB, and
	// leaving them where earlier agents wrote them keeps every deployed
	// campaign armed across the agent update.
	CampaignDir string
	// Reason is the clause the agent logs to say why Path was chosen.
	Reason string
	// LeftBehind and LeftBehindBytes describe the episodes still in LegacyRoot
	// when the store moved to DataDiskRoot. Nothing moves or deletes them;
	// they are only no longer listed.
	LeftBehind      int
	LeftBehindBytes int64
	LegacyPath      string
}

// LeftBehindMessage is the one log line about episodes an earlier agent
// recorded at LegacyRoot, or "" when there are none.
func (l StoreLocation) LeftBehindMessage() string {
	if l.LeftBehind == 0 {
		return ""
	}
	noun, verb, pronoun := "episodes", "remain", "them"
	if l.LeftBehind == 1 {
		noun, verb, pronoun = "episode", "remains", "it"
	}
	return fmt.Sprintf("Wendy Data now stores episodes in %s. %d %s (%s) recorded before that %s in %s, no longer listed; nothing was moved or deleted. Move %s into %s while the agent is stopped to list %s again, or delete %s to free the root disk.",
		l.Path, l.LeftBehind, noun, humanBytes(l.LeftBehindBytes), verb, l.LegacyPath, pronoun, l.Path, pronoun, pronoun)
}

// explicitStoreLocation is the location of a store whose path was given, which
// in the agent means WENDY_DATA_DIR. Everything stays beside it, exactly as it
// did before the default moved.
func explicitStoreLocation(root string) StoreLocation {
	return StoreLocation{Path: root, CampaignDir: filepath.Join(filepath.Dir(root), "campaigns"), Reason: "WENDY_DATA_DIR names it"}
}

// fsIdentity is what the store choice compares: which filesystem holds a path,
// and how big that filesystem is.
type fsIdentity struct {
	device uint64
	size   int64
}

// filesystemOf is a variable so tests can present a separate, larger data
// filesystem on a host that has only one.
var filesystemOf = statFilesystem

// storeLayout names the paths the default store is chosen between. The agent
// uses defaultStoreLayout; tests substitute temporary directories.
type storeLayout struct {
	// dataMount is the directory WendyOS bind-mounts from /data/wendy.
	dataMount string
	dataDisk  string
	legacy    string
	// system is the root filesystem the data mount must differ from.
	system string
	// dataPartition is named in the advice of a space error, and only when
	// it really is a separate filesystem from system.
	dataPartition string
}

var defaultStoreLayout = storeLayout{
	dataMount:     "/var/lib/wendy",
	dataDisk:      DataDiskRoot,
	legacy:        LegacyRoot,
	system:        "/",
	dataPartition: "/data",
}

// choose picks the default store. DataDiskRoot wins only when it is really on
// a different filesystem from the root one, and a larger one; anything that
// cannot be confirmed keeps LegacyRoot, which is what every earlier agent did.
func (l storeLayout) choose() StoreLocation {
	keep := func(why string) StoreLocation {
		return StoreLocation{
			Path:        l.legacy,
			CampaignDir: filepath.Join(filepath.Dir(l.legacy), "campaigns"),
			Reason:      "kept the root-disk default: " + why,
		}
	}
	if info, err := os.Stat(l.dataMount); err != nil || !info.IsDir() {
		return keep(l.dataMount + " does not exist, so this is not a WendyOS device with a data partition")
	}
	data, err := filesystemOf(nearestExisting(l.dataDisk))
	if err != nil {
		return keep(fmt.Sprintf("could not inspect %s: %v", l.dataMount, err))
	}
	system, err := filesystemOf(l.system)
	if err != nil {
		return keep(fmt.Sprintf("could not inspect the root filesystem: %v", err))
	}
	if data.device == system.device {
		return keep(l.dataMount + " is on the root filesystem")
	}
	if data.size <= system.size {
		return keep(fmt.Sprintf("%s is on its own filesystem, but it (%s) is not larger than the root filesystem (%s)", l.dataMount, humanBytes(data.size), humanBytes(system.size)))
	}
	if err := os.MkdirAll(l.dataDisk, 0o750); err != nil {
		return keep(fmt.Sprintf("could not create %s: %v", l.dataDisk, err))
	}
	location := StoreLocation{
		Path:        l.dataDisk,
		CampaignDir: filepath.Join(filepath.Dir(l.legacy), "campaigns"),
		Reason:      fmt.Sprintf("%s is on its own filesystem (%s), larger than the root filesystem (%s)", l.dataMount, humanBytes(data.size), humanBytes(system.size)),
		LegacyPath:  l.legacy,
	}
	location.LeftBehind, location.LeftBehindBytes = countEpisodes(l.legacy)
	return location
}

// suggestDataPartition reports whether a space error for the store at root
// should name the data partition: the store is on the root filesystem, and the
// data partition is a separate one. Advice to move to a disk that is not there,
// or that the store is already on, would only mislead.
func (l storeLayout) suggestDataPartition(root string) bool {
	store, err := filesystemOf(nearestExisting(root))
	if err != nil {
		return false
	}
	system, err := filesystemOf(l.system)
	if err != nil || store.device != system.device {
		return false
	}
	if info, err := os.Stat(l.dataPartition); err != nil || !info.IsDir() {
		return false
	}
	data, err := filesystemOf(l.dataPartition)
	return err == nil && data.device != system.device
}

// nearestExisting returns path, or its closest ancestor that exists, so a
// store directory that has not been created yet is judged by the filesystem
// it would be created on.
func nearestExisting(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// countEpisodes counts the episode directories in a store and the bytes they
// hold, including any an earlier agent left mid-recording. Hidden entries are
// the store's own bookkeeping (the app-stream journal, event inboxes), not
// episodes.
func countEpisodes(root string) (int, int64) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}
	var count int
	var size int64
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		count++
		size += dirBytes(filepath.Join(root, e.Name()))
	}
	return count, size
}

// StoreSpaceError is the episode store refusing a new episode for lack of
// room. The message rounds every size for a person; the fields keep the exact
// byte counts.
type StoreSpaceError struct {
	// Path is the episode store.
	Path string
	// Free is the free space on the store's filesystem once eviction has done
	// what it can, and Reserve is the free space eviction must preserve.
	Free, Reserve int64
	// Used is what the store holds once eviction has done what it can, and
	// Quota is the most it may hold: a fifth of the filesystem, capped at
	// MaxQuota.
	Used, Quota, MaxQuota int64
	// Recording is the part of Used in episodes still recording, which
	// eviction never touches.
	Recording int64
	// SuggestDataPartition adds "under /data" to the advice, set when the
	// store is on the root filesystem and a separate data partition exists.
	SuggestDataPartition bool
}

func (e *StoreSpaceError) Error() string {
	disk := "a bigger disk"
	if e.SuggestDataPartition {
		disk = "a bigger disk (e.g. under /data)"
	}
	if e.Free < e.Reserve {
		msg := fmt.Sprintf("Wendy Data can't record: the disk holding %s has %s free and needs %s free. Point WENDY_DATA_DIR at a folder on %s or lower WENDY_DATA_RESERVE_BYTES.",
			e.Path, humanBytes(e.Free), humanBytes(e.Reserve), disk)
		if e.Used > e.Quota {
			msg += fmt.Sprintf(" The recordings there also take %s, over the store's %s limit.", humanBytes(e.Used), humanBytes(e.Quota))
		}
		return msg
	}
	held := "none of them can be removed to make room yet"
	if e.Recording > 0 {
		held = fmt.Sprintf("none of them can be removed to make room yet (%s is still being recorded)", humanBytes(e.Recording))
	}
	// The quota is the smaller of a fifth of the disk and MaxQuota, so raising
	// WENDY_DATA_MAX_BYTES helps only when the cap is what binds.
	way := "point WENDY_DATA_DIR at a folder on " + disk
	if e.Quota == e.MaxQuota {
		way = "raise WENDY_DATA_MAX_BYTES"
	}
	return fmt.Sprintf("Wendy Data can't record: the recordings in %s take %s, over the store's %s limit, and %s. Wait for recordings, uploads and downloads in progress to finish, or %s.",
		e.Path, humanBytes(e.Used), humanBytes(e.Quota), held, way)
}

// errStoreUnreadable wraps a failure to read the store's free space.
func errStoreUnreadable(path string, err error) error {
	return fmt.Errorf("Wendy Data can't record: could not read how much space is free on the disk holding %s: %w", path, err)
}

// humanBytes renders a byte count in decimal units with at most one decimal,
// the way disk sizes are sold and the way df -H prints them: 4.1 GB, 433 GB.
func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	prefix := "kMGTPE"[exp]
	if value < 10 {
		return fmt.Sprintf("%.1f %cB", value, prefix)
	}
	return fmt.Sprintf("%.0f %cB", value, prefix)
}

// StoreLocation reports where the episode store is, where campaign plans
// live, and why, for the agent to log once at startup.
func (m *Manager) StoreLocation() StoreLocation { return m.location }
