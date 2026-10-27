package data

import (
	"sort"
	"time"
)

// EpisodeStateSealing is the state InProgress reports for an episode whose
// capture and post-seal drain are over and whose files are being remuxed and
// checksummed. Like EpisodeStateDraining it never reaches a manifest on disk:
// the episode is published as "complete" or "interrupted" when the seal ends.
const EpisodeStateSealing = "sealing"

// InProgressEpisode describes one episode that has not been published to the
// store yet. List reads only published episodes, so without this a recording
// is invisible from the moment it starts until its seal has finished.
type InProgressEpisode struct {
	ID   string
	Name string
	// State is the manifest's own state while the episode captures
	// ("recording"), EpisodeStateDraining while it serves its post-seal drain,
	// and EpisodeStateSealing while finalize remuxes and checksums it.
	State            string
	StartedUnixNanos int64
	BootID           string
	TriggerReason    string
	CampaignName     string
	// Sources are the IDs of the sources the episode records. A source a
	// campaign named that did not resolve at trigger is left out: the manifest
	// keeps it to show it was asked for, but nothing of it is being recorded.
	Sources []string
	// Duration is how long the episode has captured, on CLOCK_BOOTTIME. It
	// stops growing when the capture adapters stop, so a draining or sealing
	// episode reports the length it recorded.
	Duration time.Duration
	// SizeBytes is what the episode directory holds on disk right now.
	SizeBytes int64

	dir              string
	requestBootNanos int64
	captureStoppedNS int64
}

// inProgressViewLocked copies what InProgress reports about a. Callers hold
// m.mu, which is what makes reading a.manifest safe.
func inProgressViewLocked(a *activeEpisode, state string) InProgressEpisode {
	mf := a.manifest
	v := InProgressEpisode{
		ID: mf.ID, Name: mf.Name, State: state, StartedUnixNanos: mf.StartedUnixNanos, BootID: mf.BootID,
		TriggerReason: mf.Trigger.Reason, CampaignName: mf.Trigger.CampaignName,
		dir: a.dir, requestBootNanos: mf.RequestBootNanos, captureStoppedNS: mf.CaptureStoppedEpisodeNS,
	}
	for _, s := range mf.Sources {
		if s.DropAccounting == "source_absent_at_trigger" {
			continue
		}
		v.Sources = append(v.Sources, s.Source.ID)
	}
	return v
}

// InProgress lists every episode that has not been published yet, newest
// first: capturing ones, ones serving their post-seal drain, and ones finalize
// is sealing. Status answers a narrower question (one episode, for older
// clients) and ActiveEpisodeKeys a different one (which campaigns are busy).
//
// Sizes are measured after m.mu is released. Walking an episode directory is
// file system I/O, and m.mu is what every application record waits on.
func (m *Manager) InProgress() []InProgressEpisode {
	m.mu.Lock()
	out := make([]InProgressEpisode, 0, len(m.active)+len(m.sealing)+len(m.finalizing))
	for _, a := range m.active {
		out = append(out, inProgressViewLocked(a, a.manifest.State))
	}
	for _, a := range m.sealing {
		out = append(out, inProgressViewLocked(a, EpisodeStateDraining))
	}
	out = append(out, m.finalizing...)
	m.mu.Unlock()

	now, nowErr := readBootTime()
	for i := range out {
		v := &out[i]
		v.SizeBytes = dirBytes(v.dir)
		switch {
		case v.captureStoppedNS > 0:
			v.Duration = time.Duration(v.captureStoppedNS)
		case nowErr == nil && now > v.requestBootNanos:
			v.Duration = time.Duration(now - v.requestBootNanos)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedUnixNanos > out[j].StartedUnixNanos })
	return out
}

// forgetFinalizingLocked drops the sealing view of episode id once its seal has
// published or abandoned it. Callers hold m.mu.
func (m *Manager) forgetFinalizingLocked(id string) {
	for i, v := range m.finalizing {
		if v.ID == id {
			m.finalizing = append(m.finalizing[:i], m.finalizing[i+1:]...)
			return
		}
	}
}
