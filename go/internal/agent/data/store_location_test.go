package data

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sizes from Mesh 1, the Jetson Orin Nano the bug was found on: a 12 GB A/B
// root slot beside a 433 GB /data.
const (
	meshRootBytes = int64(12_000_000_000)
	meshDataBytes = int64(433_000_000_000)
)

// testLayout is a storeLayout under one temporary directory. The data mount
// and the data partition exist; the stores themselves do not yet.
func testLayout(t *testing.T) storeLayout {
	t.Helper()
	dir := t.TempDir()
	l := storeLayout{
		dataMount:     filepath.Join(dir, "var/lib/wendy"),
		legacy:        filepath.Join(dir, "var/lib/wendy-agent/data/episodes"),
		system:        filepath.Join(dir, "root"),
		dataPartition: filepath.Join(dir, "data"),
	}
	l.dataDisk = filepath.Join(l.dataMount, "wendy-data", "episodes")
	for _, d := range []string{l.dataMount, l.system, l.dataPartition} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

// fakeFilesystems answers filesystemOf by the longest matching path prefix,
// and with the system filesystem for anything else.
func fakeFilesystems(t *testing.T, system fsIdentity, mounts map[string]fsIdentity) {
	t.Helper()
	previous := filesystemOf
	filesystemOf = func(path string) (fsIdentity, error) {
		best, found := "", system
		for prefix, fs := range mounts {
			if (path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator))) && len(prefix) > len(best) {
				best, found = prefix, fs
			}
		}
		return found, nil
	}
	t.Cleanup(func() { filesystemOf = previous })
}

// meshFilesystems is Mesh 1: the data mount and the data partition are the
// same separate, larger filesystem, and everything else is the root slot.
func meshFilesystems(t *testing.T, l storeLayout) {
	data := fsIdentity{device: 2, size: meshDataBytes}
	fakeFilesystems(t, fsIdentity{device: 1, size: meshRootBytes}, map[string]fsIdentity{l.dataMount: data, l.dataPartition: data})
}

func useStoreLayout(t *testing.T, l storeLayout) {
	t.Helper()
	previous := defaultStoreLayout
	defaultStoreLayout = l
	t.Cleanup(func() { defaultStoreLayout = previous })
}

func TestDefaultStoreMovesToSeparateLargerFilesystem(t *testing.T) {
	l := testLayout(t)
	meshFilesystems(t, l)

	got := l.choose()
	if got.Path != l.dataDisk {
		t.Fatalf("store = %s, want the data disk %s (%s)", got.Path, l.dataDisk, got.Reason)
	}
	if want := filepath.Join(filepath.Dir(l.legacy), "campaigns"); got.CampaignDir != want {
		t.Fatalf("campaign plans = %s, want them left at %s", got.CampaignDir, want)
	}
	if want := "is on its own filesystem (433 GB), larger than the root filesystem (12 GB)"; !strings.Contains(got.Reason, want) {
		t.Fatalf("reason %q does not say %q", got.Reason, want)
	}
	if info, err := os.Stat(l.dataDisk); err != nil || !info.IsDir() {
		t.Fatalf("data disk store was not created: %v", err)
	}
}

func TestDefaultStoreKeepsLegacyRootWhenDataDiskGainsNothing(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, l storeLayout)
		reason string
	}{
		{
			name: "same filesystem as root",
			setup: func(t *testing.T, l storeLayout) {
				fakeFilesystems(t, fsIdentity{device: 1, size: meshRootBytes}, nil)
			},
			reason: "is on the root filesystem",
		},
		{
			// A WendyOS VM: 3 GiB slots beside a 512 MiB /data.
			name: "separate but smaller",
			setup: func(t *testing.T, l storeLayout) {
				fakeFilesystems(t, fsIdentity{device: 1, size: 3 << 30}, map[string]fsIdentity{l.dataMount: {device: 2, size: 512 << 20}})
			},
			reason: "is on its own filesystem, but it (537 MB) is not larger than the root filesystem (3.2 GB)",
		},
		{
			name: "data mount missing",
			setup: func(t *testing.T, l storeLayout) {
				meshFilesystems(t, l)
				if err := os.Remove(l.dataMount); err != nil {
					t.Fatal(err)
				}
			},
			reason: "does not exist, so this is not a WendyOS device with a data partition",
		},
		{
			name: "filesystem cannot be inspected",
			setup: func(t *testing.T, l storeLayout) {
				previous := filesystemOf
				filesystemOf = func(string) (fsIdentity, error) { return fsIdentity{}, errors.New("statfs: permission denied") }
				t.Cleanup(func() { filesystemOf = previous })
			},
			reason: "could not inspect",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := testLayout(t)
			tc.setup(t, l)
			got := l.choose()
			if got.Path != l.legacy {
				t.Fatalf("store = %s, want the root-disk default %s", got.Path, l.legacy)
			}
			if want := filepath.Join(filepath.Dir(l.legacy), "campaigns"); got.CampaignDir != want {
				t.Fatalf("campaign plans = %s, want %s", got.CampaignDir, want)
			}
			if !strings.HasPrefix(got.Reason, "kept the root-disk default: ") || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("reason %q does not say %q", got.Reason, tc.reason)
			}
			if _, err := os.Stat(l.dataDisk); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("data disk store was created although it was not chosen: %v", err)
			}
			if got.LeftBehindMessage() != "" {
				t.Fatalf("a store that did not move reported episodes left behind: %q", got.LeftBehindMessage())
			}
		})
	}
}

// TestNewManagerDefaultStore drives the agent's path, NewManager(""), and
// checks that a campaign an earlier agent deployed is still found once the
// episodes have moved.
func TestNewManagerDefaultStore(t *testing.T) {
	l := testLayout(t)
	meshFilesystems(t, l)
	useStoreLayout(t, l)
	campaigns := filepath.Join(filepath.Dir(l.legacy), "campaigns")
	if err := os.MkdirAll(campaigns, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(campaigns, "forklift.json"), []byte(`{"name":"forklift","state":"armed"}`), 0o640); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager("")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.StoreLocation().Path; got != l.dataDisk {
		t.Fatalf("store = %s, want %s", got, l.dataDisk)
	}
	if got := m.InferenceDirectory(); got != filepath.Join(filepath.Dir(l.dataDisk), "inference") {
		t.Fatalf("inference cache = %s, want it beside the store on the data disk", got)
	}
	deployed, err := m.Campaigns()
	if err != nil {
		t.Fatal(err)
	}
	if len(deployed) != 1 || deployed[0].Name != "forklift" {
		t.Fatalf("campaigns after the store moved = %+v, want the one deployed before", deployed)
	}
}

func TestWendyDataDirOverridesDefaultStore(t *testing.T) {
	l := testLayout(t)
	meshFilesystems(t, l)
	useStoreLayout(t, l)
	explicit := filepath.Join(t.TempDir(), "custom", "episodes")

	m, err := NewManager(explicit)
	if err != nil {
		t.Fatal(err)
	}
	got := m.StoreLocation()
	if got.Path != explicit {
		t.Fatalf("store = %s, want WENDY_DATA_DIR's %s", got.Path, explicit)
	}
	if want := filepath.Join(filepath.Dir(explicit), "campaigns"); got.CampaignDir != want {
		t.Fatalf("campaign plans = %s, want them beside WENDY_DATA_DIR at %s, as before", got.CampaignDir, want)
	}
	if got.Reason != "WENDY_DATA_DIR names it" {
		t.Fatalf("reason = %q", got.Reason)
	}
	if _, err := os.Stat(l.dataDisk); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("WENDY_DATA_DIR was set, yet the default store was created: %v", err)
	}
}

func TestLeftBehindEpisodesAreCountedAndLeftInPlace(t *testing.T) {
	l := testLayout(t)
	meshFilesystems(t, l)
	episodes := map[string]int{
		"20261025T185000Z-0123456789abcdef":         1000,
		"20261026T090000Z-fedcba9876543210.partial": 500,
	}
	for name, size := range episodes {
		dir := filepath.Join(l.legacy, name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "payload"), make([]byte, size), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	// Store bookkeeping is not an episode.
	if err := os.MkdirAll(filepath.Join(l.legacy, ".recordings", "app"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.legacy, ".device-events.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := l.choose()
	if got.LeftBehind != 2 || got.LeftBehindBytes != 1500 {
		t.Fatalf("left behind = %d episodes, %d bytes; want 2 and 1500", got.LeftBehind, got.LeftBehindBytes)
	}
	want := "Wendy Data now stores episodes in " + l.dataDisk + ". 2 episodes (1.5 kB) recorded before that remain in " + l.legacy +
		", no longer listed; nothing was moved or deleted. Move them into " + l.dataDisk +
		" while the agent is stopped to list them again, or delete them to free the root disk."
	if msg := got.LeftBehindMessage(); msg != want {
		t.Fatalf("left-behind line:\n got %q\nwant %q", msg, want)
	}
	for name := range episodes {
		if _, err := os.Stat(filepath.Join(l.legacy, name, "payload")); err != nil {
			t.Fatalf("episode %s at the old location was touched: %v", name, err)
		}
	}

	one := StoreLocation{Path: "/new", LegacyPath: "/old", LeftBehind: 1, LeftBehindBytes: 2_500_000_000}
	if msg := one.LeftBehindMessage(); !strings.Contains(msg, " 1 episode (2.5 GB) recorded before that remains in /old, no longer listed;") || !strings.Contains(msg, "Move it into /new") {
		t.Fatalf("single episode line: %q", msg)
	}
}

func TestStoreSpaceErrorText(t *testing.T) {
	cases := []struct {
		name string
		err  StoreSpaceError
		want string
	}{
		{
			// The failure seen on Mesh 1 before this change: an empty store on
			// the root slot, which has less free than the reserve.
			name: "reserve on the root disk",
			err: StoreSpaceError{Path: LegacyRoot, Free: 4_100_000_000, Reserve: DefaultReserveBytes,
				Used: 0, Quota: 2393926860, MaxQuota: DefaultMaxQuotaBytes, SuggestDataPartition: true},
			want: "Wendy Data can't record: the disk holding /var/lib/wendy-agent/data/episodes has 4.1 GB free and needs 5.4 GB free. " +
				"Point WENDY_DATA_DIR at a folder on a bigger disk (e.g. under /data) or lower WENDY_DATA_RESERVE_BYTES.",
		},
		{
			name: "reserve already on the data disk",
			err: StoreSpaceError{Path: DataDiskRoot, Free: 3_000_000_000, Reserve: DefaultReserveBytes,
				Used: 60_000_000_000, Quota: 50_000_000_000, MaxQuota: DefaultMaxQuotaBytes},
			want: "Wendy Data can't record: the disk holding /var/lib/wendy/wendy-data/episodes has 3.0 GB free and needs 5.4 GB free. " +
				"Point WENDY_DATA_DIR at a folder on a bigger disk or lower WENDY_DATA_RESERVE_BYTES. " +
				"The recordings there also take 60 GB, over the store's 50 GB limit.",
		},
		{
			name: "quota held by a recording in progress",
			err: StoreSpaceError{Path: DataDiskRoot, Free: 300_000_000_000, Reserve: DefaultReserveBytes,
				Used: 90_000_000_000, Quota: 86_600_000_000, MaxQuota: DefaultMaxQuotaBytes, Recording: 1_200_000_000},
			want: "Wendy Data can't record: the recordings in /var/lib/wendy/wendy-data/episodes take 90 GB, over the store's 87 GB limit, " +
				"and none of them can be removed to make room yet (1.2 GB is still being recorded). " +
				"Wait for recordings, uploads and downloads in progress to finish, or point WENDY_DATA_DIR at a folder on a bigger disk.",
		},
		{
			name: "quota capped by WENDY_DATA_MAX_BYTES",
			err: StoreSpaceError{Path: DataDiskRoot, Free: 300_000_000_000, Reserve: DefaultReserveBytes,
				Used: 2_000_000_000, Quota: 1_000_000_000, MaxQuota: 1_000_000_000},
			want: "Wendy Data can't record: the recordings in /var/lib/wendy/wendy-data/episodes take 2.0 GB, over the store's 1.0 GB limit, " +
				"and none of them can be removed to make room yet. Wait for recordings, uploads and downloads in progress to finish, or raise WENDY_DATA_MAX_BYTES.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestStartReportsStoreSpaceError checks the error a recording or a campaign
// trigger actually gets: Start is the path both take.
func TestStartReportsStoreSpaceError(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetWarnLogger(func(string) {})
	// No real filesystem has an exabyte free, so the reserve cannot be met.
	m.SetQuota(0, 1<<60)
	_, err = m.Start(StartOptions{Sources: []string{"applications"}})
	var space *StoreSpaceError
	if !errors.As(err, &space) {
		t.Fatalf("Start error = %v, want a *StoreSpaceError", err)
	}
	if space.Path != root || space.Reserve != 1<<60 || space.Free >= space.Reserve {
		t.Fatalf("error fields = %+v", space)
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "Wendy Data can't record: the disk holding "+root+" has ") ||
		!strings.Contains(msg, "needs 1.2 EB free") || !strings.Contains(msg, "WENDY_DATA_RESERVE_BYTES") {
		t.Fatalf("message = %q", msg)
	}
}

func TestSuggestDataPartition(t *testing.T) {
	l := testLayout(t)
	meshFilesystems(t, l)
	if !l.suggestDataPartition(l.legacy) {
		t.Fatal("a store on the root slot beside a separate /data should be pointed at /data")
	}
	if l.suggestDataPartition(l.dataDisk) {
		t.Fatal("a store already on the data disk must not be told to move there")
	}
	if err := os.Remove(l.dataPartition); err != nil {
		t.Fatal(err)
	}
	if l.suggestDataPartition(l.legacy) {
		t.Fatal("advice named a /data that does not exist")
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{
		0:                    "0 B",
		999:                  "999 B",
		1500:                 "1.5 kB",
		2393926860:           "2.4 GB",
		DefaultReserveBytes:  "5.4 GB",
		DefaultMaxQuotaBytes: "54 GB",
		433_000_000_000:      "433 GB",
	} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
