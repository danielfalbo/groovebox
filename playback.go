package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// QueueEntry is one item in the server-side playback queue.
type QueueEntry struct {
	FileID   string `json:"file_id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	AlbumID  string `json:"album_id,omitempty"`
	Album    string `json:"album,omitempty"`
	Path     string `json:"-"`
	CoverURL string `json:"cover_url,omitempty"`
	Duration int64  `json:"duration_ms"`
	Kind     string `json:"kind"` // 'track' or 'raw'
}

// Player holds the authoritative server-side playback state. Groovebox OWNS
// ALSA hw:0; a browser tab is just a best-effort mirror of this state, which
// lives entirely server-side.
type Player struct {
	mu sync.Mutex

	queue    []QueueEntry
	index    int // current queue position; -1 = idle
	status   string // "idle" | "playing" | "paused" | "failed"
	baseMs   int64 // position at the most recent (re)start
	startedAt time.Time
	pausedAt  time.Time
	cmd       *exec.Cmd
	pgid      int
	volume    int

	sess uint64 // bumped on each (re)launch; stale watch goroutines bail out
}

var player = &Player{index: -1, status: "idle", volume: 80}

// volumeRe extracts the percentage from `amixer sget Master` output
// (e.g. `Mono: Playback 68 [78%]`).
var volumeRe = regexp.MustCompile(`\[(\d+)%\]`)

// readAlsaVolume reads the real ALSA Master volume (0-100) from the hardware,
// or -1 if it can't be read. We read from the hardware rather than trusting our
// in-memory value so the now-playing bar never shows a stale/misleading level.
func readAlsaVolume() int {
	out, err := exec.Command("amixer", "-D", "hw:0", "sget", "Master").Output()
	if err != nil {
		return -1
	}
	m := volumeRe.FindSubmatch(out)
	if m == nil {
		return -1
	}
	v, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return -1
	}
	if v < 0 {
		v = 0
	} else if v > 100 {
		v = 100
	}
	return v
}

func init() {
	// Seed the in-memory volume from the actual hardware so a freshly restarted
	// server mirrors reality (e.g. a muted/zeroed ALSA Master) instead of always
	// claiming 80%. Otherwise playback can be silent while the bar shows 80%.
	if v := readAlsaVolume(); v >= 0 {
		player.volume = v
	}
}

// quoteShellArg wraps a string for use as one POSIX shell argument.
func quoteShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ffmpegArgs builds the ffmpeg decoding pipeline arguments for a file/offset.
func ffmpegArgs(path string, seekMs int64) []string {
	ff := []string{"-v", "error"}
	if seekMs > 0 {
		ff = append(ff, "-ss", fmt.Sprintf("%.3f", float64(seekMs)/1000.0))
	}
	// decode + resample to S16LE 44.1k stereo (ALC897 accepts S16_LE/S32_LE).
	ff = append(ff, "-i", path, "-f", "s16le", "-ac", "2", "-ar", "44100", "-")
	return ff
}

// launch kills any current audio and starts the ffmpeg -> aplay pipeline.
func (p *Player) launch(entry QueueEntry) error {
	p.stopProcessLocked()

	ff := ffmpegArgs(entry.Path, p.baseMs)
	quoted := make([]string, 0, len(ff))
	for _, a := range ff {
		quoted = append(quoted, quoteShellArg(a))
	}
	pipe := "ffmpeg " + strings.Join(quoted, " ") + " | aplay -D hw:0 -q -f S16_LE -r 44100 -c 2"

	cmd := exec.Command("bash", "-c", pipe)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	p.pgid = cmd.Process.Pid
	p.sess++ // mark this process as the current session
	p.status = "playing"
	p.startedAt = time.Now()
	p.pausedAt = time.Time{}
	return nil
}

// startLocked begins playing at the current queue index (mu held).
func (p *Player) startLocked() error {
	if p.index < 0 || p.index >= len(p.queue) {
		p.status = "idle"
		return nil
	}
	p.applyVolumeLocked() // (re)sync hardware to our volume so audio starts audible
	if err := p.launch(p.queue[p.index]); err != nil {
		p.status = "failed"
		return err
	}
	go p.watch(p.sess)
	return nil
}

// watch advances to the next queue item when the current one exits naturally.
// sess is the launcher's session id: if a stop/seek/next has relaunched a new
// process underneath (sess bumped), this stale watcher must not act.
func (p *Player) watch(sess uint64) {
	cm := p.cmd
	if cm == nil {
		return
	}
	_ = cm.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess != sess || p.status != "playing" {
		return // superseded or explicitly stopped/paused
	}
	if p.index < len(p.queue)-1 {
		p.index++
		p.baseMs = 0
		_ = p.startLocked()
	} else {
		p.status = "idle"
		p.baseMs = 0
	}
}

// stopProcessLocked kills the whole process group (ffmpeg + aplay). It does not
// Wait() here: each launched pipeline has its own watch goroutine that reaps it.
// We SIGTERM and let that goroutine finish the Wait, guarded by the sess epoch.
// Before returning, it waits for the old group to actually terminate and release
// ALSA hw:0 — so a relaunch (seek/skip) starting a new aplay right afterwards
// won't fail with "Device or resource busy" from the previous pipeline still
// holding the device.
func (p *Player) stopProcessLocked() {
	oldPgid := p.pgid
	if p.cmd != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-oldPgid, syscall.SIGTERM)
	}
	p.cmd = nil
	p.pgid = 0
	p.startedAt = time.Time{}
	p.pausedAt = time.Time{}

	// Wait (briefly) for the old process group to fully disappear so hw:0 is
	// free before any replacement pipeline is spawned.
	if oldPgid > 0 {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			// kill(-pgid, 0) is only success while a live member remains.
			if err := syscall.Kill(-oldPgid, 0); err == syscall.ESRCH {
				return // group gone: device released
			}
			time.Sleep(15 * time.Millisecond)
		}
	}
}

// posLocked returns current virtual position in ms.
func (p *Player) posLocked() int64 {
	switch p.status {
	case "idle":
		return 0
	case "paused":
		return p.baseMs + p.pausedAt.Sub(p.startedAt).Milliseconds()
	default: // playing
		return p.baseMs + time.Since(p.startedAt).Milliseconds()
	}
}

// setLocked is a helper to acquire the lock safely.
func (p *Player) lock()   { p.mu.Lock() }
func (p *Player) unlock() { p.mu.Unlock() }

// Pause freezes audio (SIGSTOP), keeping position.
func (p *Player) Pause() {
	p.lock()
	defer p.unlock()
	if p.status != "playing" || p.cmd == nil {
		return
	}
	_ = syscall.Kill(-p.pgid, syscall.SIGSTOP)
	p.pausedAt = time.Now()
	p.status = "paused"
}

// Resume unpauses (SIGCONT).
func (p *Player) Resume() {
	p.lock()
	defer p.unlock()
	if p.status != "paused" || p.cmd == nil {
		return
	}
	_ = syscall.Kill(-p.pgid, syscall.SIGCONT)
	p.startedAt = time.Now()
	p.pausedAt = time.Time{}
	p.status = "playing"
}

// Stop halts playback and clears state.
func (p *Player) Stop() {
	p.lock()
	defer p.unlock()
	p.stopProcessLocked()
	p.index = -1
	p.status = "idle"
	p.baseMs = 0
	p.queue = nil
}

// Clear empties the queue.
func (p *Player) Clear() {
	p.lock()
	defer p.unlock()
	p.stopProcessLocked()
	p.index = -1
	p.status = "idle"
	p.baseMs = 0
	p.queue = nil
}

// SeekTo jumps to an absolute position (ms) in the current track.
func (p *Player) SeekTo(ms int64) {
	p.lock()
	defer p.unlock()
	if p.index < 0 || p.index >= len(p.queue) {
		return
	}
	entry := p.queue[p.index]
	if entry.Duration > 0 && ms >= entry.Duration {
		ms = entry.Duration - 1
	}
	if ms < 0 {
		ms = 0
	}
	p.baseMs = ms
	if p.status == "playing" || p.status == "paused" {
		if p.status == "paused" {
			// resume first so launch doesn't complain
			p.status = "playing"
		}
		if err := p.launch(entry); err == nil {
			// Watch the relaunched pipeline too. Without this, when the seek-
			// relaunched process exits naturally (end-of-track / error) no watcher
			// is left to reap it, and the player would stay frozen at "playing"
			// with an advancing wall-clock position but dead audio underneath.
			go p.watch(p.sess)
		}
	}
}

// Next / Prev move through the queue.
func (p *Player) Next() { p.skip(1) }
func (p *Player) Prev() { p.skip(-1) }

func (p *Player) skip(delta int) {
	p.lock()
	defer p.unlock()
	if len(p.queue) == 0 {
		return
	}
	p.index += delta
	if p.index < 0 {
		p.index = len(p.queue) - 1
	}
	if p.index >= len(p.queue) {
		p.index = 0
	}
	p.baseMs = 0
	if p.status == "playing" || p.status == "paused" {
		_ = p.startLocked()
	}
}

// PlayQueue sets the queue and plays from idx (0-based).
func (p *Player) PlayQueue(list []QueueEntry, idx int) {
	p.lock()
	defer p.unlock()
	p.stopProcessLocked()
	p.queue = list
	p.index = -1
	p.baseMs = 0
	if len(list) == 0 {
		p.status = "idle"
		return
	}
	if idx < 0 || idx >= len(list) {
		idx = 0
	}
	p.index = idx
	_ = p.startLocked()
}

// Enqueue appends entries, starting playback if currently idle.
func (p *Player) Enqueue(entries []QueueEntry) {
	p.lock()
	defer p.unlock()
	p.queue = append(p.queue, entries...)
	if p.status == "idle" && len(p.queue) > 0 {
		p.index = 0
		_ = p.startLocked()
	}
}

// applyVolumeLocked pushes p.volume to ALSA (mu held). Best-effort.
func (p *Player) applyVolumeLocked() {
	for _, ctrl := range []string{"Master", "PCM"} {
		_ = exec.Command("amixer", "-D", "hw:0", "sset", ctrl, strconv.Itoa(p.volume)+"%").Run()
	}
}

// SetVolume adjusts ALSA output (0-100) and records it as the new in-memory
// level so the now-playing bar and the hardware always agree.
func (p *Player) SetVolume(v int) {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	p.lock()
	p.volume = v
	p.applyVolumeLocked()
	p.unlock()
}

// Snapshot is a serializable view of playback state.
type Snapshot struct {
	Status   string       `json:"status"`
	Index    int          `json:"index"`
	Queue    []QueueEntry `json:"queue"`
	Position int64        `json:"position_ms"`
	Current  *QueueEntry  `json:"current,omitempty"`
	Volume   int          `json:"volume"`
}

func (p *Player) snapshotLocked() Snapshot {
	s := Snapshot{Status: p.status, Index: p.index, Queue: p.queue, Volume: p.volume}
	s.Position = p.posLocked()
	if p.index >= 0 && p.index < len(p.queue) {
		cur := p.queue[p.index]
		s.Current = &cur
	}
	return s
}

// State returns a snapshot for GET /api/playback/state.
func (p *Player) State() Snapshot {
	p.lock()
	defer p.unlock()
	return p.snapshotLocked()
}