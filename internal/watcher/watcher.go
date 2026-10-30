// Package watcher watches Claude Code JSONL session files for new content.
package watcher

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/zxela/claude-monitor/internal/transcript"
)

// bufPool reuses read buffers to avoid allocating 64KB on every readNewLines call.
var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 64*1024)
		return &b
	},
}

// homeDir caches the result of os.UserHomeDir so it is only called once.
var (
	homeDirOnce sync.Once
	homeDirVal  string
	homeDirErr  error
)

const (
	pollInterval       = 5 * time.Second
	activePollInterval = 500 * time.Millisecond
	activeWindow       = 30 * time.Second // how long after last emit a file is considered "active"
)

// defaultBasePaths are the well-known locations Claude Code writes session files.
var defaultBasePaths = []string{
	"~/.claude/projects/",
	"/home/node/.claude/projects/",
	"/root/.claude/projects/",
}

// Event carries a single parsed line from a JSONL file together with its
// session metadata.
type Event struct {
	SessionID  string
	ProjectDir string
	FilePath   string
	Line       []byte
	// Label is an optional prefix set when the path was added via Add (e.g. a
	// Docker container name). Empty for default/extra paths.
	Label string
	// Bootstrap is true for events emitted from historical data read on startup.
	Bootstrap bool
	// Workflow/agent identity from transcript.Classify(FilePath).
	// AgentID == SessionID for a Subagent or Workflow agent.
	WorkflowID string
	AgentID    string
	AgentKind  string
	// Offset is the byte just past this line in FilePath, and Inode the file it
	// was read from. Zero when the line did not come from a tracked file.
	Offset int64
	Inode  uint64
}

// FileOffset is a saved read position in a JSONL file.
type FileOffset struct {
	Offset int64
	Inode  uint64
}

// fileState tracks our read position within a single JSONL file.
type fileState struct {
	path     string
	offset   int64
	inode    uint64
	lineEnd  int64     // offset just past the line being emitted
	partial  []byte    // incomplete line carried across reads
	lastEmit time.Time // last time an event was emitted for this file
}

// Watcher monitors JSONL files and emits Events for each new line.
type Watcher struct {
	fsw           *fsnotify.Watcher
	mu            sync.Mutex
	basePaths     []string          // directories to scan
	labels        map[string]string // basePath -> label (container name)
	states        map[string]*fileState
	events        chan Event
	done          <-chan struct{} // ctx.Done() of Start; unblocks emit on shutdown
	droppedEvents atomic.Int64
	bootstrapping bool        // true during initial scan
	bootstrapCB   func(Event) // called synchronously for bootstrap events
	resume        map[string]FileOffset
	restore       func(sessionIDs []string) map[string]bool
}

// New creates a Watcher that will scan basePaths plus any extraPaths provided.
func New(extraPaths []string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(defaultBasePaths)+len(extraPaths))
	paths = append(paths, defaultBasePaths...)
	paths = append(paths, extraPaths...)

	return &Watcher{
		fsw:       fsw,
		basePaths: paths,
		labels:    make(map[string]string),
		states:    make(map[string]*fileState),
		events:    make(chan Event, 4096),
	}, nil
}

// SetBootstrapCallback sets a function to call synchronously for each historical
// line during the initial scan, bypassing the event channel.
func (w *Watcher) SetBootstrapCallback(cb func(Event)) {
	w.bootstrapCB = cb
}

// Resume makes the bootstrap scan continue each file in offsets from its saved
// position instead of re-reading it from the start. Before any line is emitted,
// restore is called with the session IDs of the files that can be resumed and
// returns the ones it restored; every other file is read in full. Call before
// Start; it has no effect without a bootstrap callback.
func (w *Watcher) Resume(offsets map[string]FileOffset, restore func(sessionIDs []string) map[string]bool) {
	w.resume = offsets
	w.restore = restore
}

// Add begins watching path at runtime, tagging sessions from that path with
// label (e.g. a Docker container name). If path is already watched, it is a
// no-op.
func (w *Watcher) Add(path, label string) {
	w.mu.Lock()
	for _, p := range w.basePaths {
		if p == path {
			w.mu.Unlock()
			return // already present
		}
	}
	w.basePaths = append(w.basePaths, path)
	if label != "" {
		w.labels[path] = label
	}
	w.mu.Unlock()

	// Scan outside the lock so we don't stall the run goroutine during a
	// potentially slow directory walk. fsnotify.Watcher is goroutine-safe.
	w.scanPathNoLock(path)
}

// Remove stops watching path and discards tracked state for all files under it.
func (w *Watcher) Remove(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Remove from basePaths.
	updated := w.basePaths[:0]
	for _, p := range w.basePaths {
		if p != path {
			updated = append(updated, p)
		}
	}
	w.basePaths = updated
	delete(w.labels, path)

	// Collect directories that housed files under the removed path, then delete
	// their file states.
	expanded := expandHome(path)
	removedDirs := make(map[string]struct{})
	for filePath := range w.states {
		if strings.HasPrefix(filePath, expanded) {
			removedDirs[filepath.Dir(filePath)] = struct{}{}
			delete(w.states, filePath)
		}
	}

	// Only remove a directory watch if no remaining tracked file lives there.
	// (Two base paths can share a parent directory; removing the watch would
	// silently kill events for the other path's files.)
	for dir := range removedDirs {
		stillNeeded := false
		for remaining := range w.states {
			if filepath.Dir(remaining) == dir {
				stillNeeded = true
				break
			}
		}
		if !stillNeeded {
			_ = w.fsw.Remove(dir)
		}
	}
}

// FindSessionFile searches all watched base paths for a JSONL file belonging
// to the given session ID. Returns the file path or "" if not found.
func (w *Watcher) FindSessionFile(sessionID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check already-tracked files first.
	for filePath := range w.states {
		base := filepath.Base(filePath)
		name := strings.TrimSuffix(base, ".jsonl")
		if name == sessionID {
			return filePath
		}
	}

	// Walk base paths for an untracked file.
	target := sessionID + ".jsonl"
	for _, base := range w.basePaths {
		expanded := expandHome(base)
		var found string
		if walkErr := filepath.WalkDir(expanded, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if filepath.Base(path) == target {
				found = path
				return filepath.SkipAll
			}
			return nil
		}); walkErr != nil {
			log.Printf("watcher: walk error (%s): %v", expanded, walkErr)
		}
		if found != "" {
			return found
		}
	}
	return ""
}

// Start begins watching and returns a channel of Events. It runs until ctx
// is cancelled.
func (w *Watcher) Start(ctx context.Context) <-chan Event {
	w.done = ctx.Done()
	go w.run(ctx)
	return w.events
}

func (w *Watcher) run(ctx context.Context) {
	defer close(w.events)

	// Initial scan — read all existing data to bootstrap session stats.
	start := time.Now()
	w.mu.Lock()
	w.bootstrapping = true
	w.scanAll()
	resumed := w.applyResume()
	// After tracking files, read their existing content. Stop early on
	// shutdown: this replay can take tens of seconds on a large history.
	for _, state := range w.states {
		if ctx.Err() != nil {
			break
		}
		w.readNewLines(state.path)
	}
	w.bootstrapping = false
	tracked := len(w.states)
	w.mu.Unlock()
	log.Printf("watcher: bootstrap read %d files (%d resumed from saved offsets) in %s",
		tracked, resumed, time.Since(start).Round(time.Millisecond))

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	activeTicker := time.NewTicker(activePollInterval)
	defer activeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.fsw.Close()
			return

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Write) && strings.HasSuffix(ev.Name, ".jsonl") {
				w.mu.Lock()
				w.readNewLines(ev.Name)
				w.mu.Unlock()
			}
			if ev.Has(fsnotify.Create) && strings.HasSuffix(ev.Name, ".jsonl") {
				w.mu.Lock()
				w.ensureTracked(ev.Name)
				w.readNewLines(ev.Name)
				w.mu.Unlock()
			}

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watcher fsnotify error: %v", err)

		case <-activeTicker.C:
			// Fast re-read for recently active session files.
			w.mu.Lock()
			now := time.Now()
			for _, state := range w.states {
				if now.Sub(state.lastEmit) < activeWindow {
					w.readNewLines(state.path)
				}
			}
			w.mu.Unlock()

		case <-ticker.C:
			// Poll for newly created JSONL files.
			w.mu.Lock()
			w.scanAll()
			w.mu.Unlock()
		}
	}
}

// applyResume moves tracked files to their saved offsets once their sessions
// are restored, and returns how many it moved. Caller must hold w.mu.
func (w *Watcher) applyResume() int {
	offsets, restore := w.resume, w.restore
	w.resume, w.restore = nil, nil
	if len(offsets) == 0 || restore == nil || w.bootstrapCB == nil {
		return 0
	}
	bySession := make(map[string][]*fileState)
	for path, saved := range offsets {
		state, ok := w.states[path]
		if !ok || !resumable(path, saved) {
			continue
		}
		sid := sessionIDFromPath(path)
		bySession[sid] = append(bySession[sid], state)
	}
	if len(bySession) == 0 {
		return 0
	}
	ids := make([]string, 0, len(bySession))
	for sid := range bySession {
		ids = append(ids, sid)
	}
	restored := restore(ids)
	n := 0
	for sid, states := range bySession {
		if !restored[sid] {
			continue
		}
		for _, state := range states {
			saved := offsets[state.path]
			state.offset = saved.Offset
			state.inode = saved.Inode
			n++
		}
	}
	return n
}

// resumable reports whether path is still the file saved was read from: same
// inode, at least as long, and saved.Offset falls just after a newline.
func resumable(path string, saved FileOffset) bool {
	if saved.Offset <= 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if info.Size() < saved.Offset || (saved.Inode != 0 && inodeOf(info) != saved.Inode) {
		log.Printf("watcher: %s was truncated or replaced since its offset was saved, reading from start", path)
		return false
	}
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, saved.Offset-1); err != nil || b[0] != '\n' {
		log.Printf("watcher: saved offset %d in %s is not at a line boundary, reading from start", saved.Offset, path)
		return false
	}
	return true
}

// scanAll walks all base paths looking for JSONL files not yet tracked.
// Caller must hold w.mu.
func (w *Watcher) scanAll() {
	for _, base := range w.basePaths {
		w.scanPath(base)
	}

	// Clean up states for files that no longer exist on disk.
	// Batch by parent directory to avoid per-file os.Stat calls.
	dirFiles := make(map[string]map[string]bool)
	for path := range w.states {
		dir := filepath.Dir(path)
		if _, ok := dirFiles[dir]; !ok {
			entries, err := os.ReadDir(dir)
			if err != nil {
				dirFiles[dir] = map[string]bool{}
				continue
			}
			m := make(map[string]bool, len(entries))
			for _, e := range entries {
				m[e.Name()] = true
			}
			dirFiles[dir] = m
		}
	}
	for path := range w.states {
		if !dirFiles[filepath.Dir(path)][filepath.Base(path)] {
			log.Printf("watcher: removing stale state for deleted file %s", path)
			delete(w.states, path)
		}
	}
}

// scanPath walks a single base path looking for JSONL files not yet tracked.
// Caller must hold w.mu.
func (w *Watcher) scanPath(base string) {
	expanded := expandHome(base)
	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		return
	}
	if err := filepath.WalkDir(expanded, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			w.ensureTracked(path)
		}
		return nil
	}); err != nil {
		log.Printf("watcher walk error (%s): %v", expanded, err)
	}
}

// scanPathNoLock walks base and registers any JSONL files found without holding
// w.mu for the duration of the walk. It acquires w.mu only briefly per file so
// the run goroutine is not stalled during a slow directory traversal.
// Safe to call concurrently with run. Used by Add.
func (w *Watcher) scanPathNoLock(base string) {
	expanded := expandHome(base)
	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		return
	}
	if walkErr := filepath.WalkDir(expanded, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil
		}
		dir := filepath.Dir(path)
		// fsnotify.Watcher is goroutine-safe; add the watch outside the lock.
		if err := w.fsw.Add(dir); err != nil {
			log.Printf("watcher add dir error (%s): %v", dir, err)
		}
		// Hold the lock only long enough to update the state map.
		w.mu.Lock()
		if _, ok := w.states[path]; !ok {
			w.states[path] = &fileState{path: path, offset: info.Size()}
		}
		w.mu.Unlock()
		return nil
	}); walkErr != nil {
		log.Printf("watcher: walk error (%s): %v", expanded, walkErr)
	}
}

// ensureTracked registers a JSONL file for watching if not already tracked.
// Files are read from the beginning so historical data bootstraps session stats.
func (w *Watcher) ensureTracked(path string) {
	if _, ok := w.states[path]; ok {
		return
	}

	if w.bootstrapCB != nil {
		// When a bootstrap callback is set, read from the beginning
		// so historical data can be used to build initial session stats.
		w.states[path] = &fileState{path: path, offset: 0}
	} else {
		// No bootstrap callback — start from EOF (only emit new lines).
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		w.states[path] = &fileState{path: path, offset: info.Size()}
	}

	// Watch the parent directory (fsnotify watches dirs, not individual files).
	dir := filepath.Dir(path)
	if err := w.fsw.Add(dir); err != nil {
		log.Printf("watcher add dir error (%s): %v", dir, err)
	}
}

// readNewLines reads any bytes appended to path since our last offset and emits
// complete JSONL lines as Events.
func (w *Watcher) readNewLines(path string) {
	state, ok := w.states[path]
	if !ok {
		// New file appeared via fsnotify before the poll cycle; track it.
		w.ensureTracked(path)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		log.Printf("watcher open error (%s): %v", path, err)
		return
	}
	defer f.Close()

	// Detect truncation or replacement: smaller than our offset, or a different
	// file at the same path.
	if info, err := f.Stat(); err == nil {
		ino := inodeOf(info)
		if info.Size() < state.offset || (state.inode != 0 && ino != state.inode) {
			log.Printf("watcher: file truncated or replaced (%s), resetting offset from %d to 0", path, state.offset)
			state.offset = 0
			state.partial = nil
		}
		state.inode = ino
	}

	if _, err := f.Seek(state.offset, 0); err != nil {
		log.Printf("watcher seek error (%s): %v", path, err)
		return
	}

	bufp := bufPool.Get().(*[]byte)
	buf := *bufp
	defer bufPool.Put(bufp)
	partial := state.partial
	state.partial = nil

	for {
		n, err := f.Read(buf)
		if n > 0 {
			// File position of partial[0]; its bytes are already in state.offset.
			pos := state.offset - int64(len(partial))
			partial = append(partial, buf[:n]...)
			chunk := partial
			partial = nil

			lines := bytes.Split(chunk, []byte("\n"))
			// The last element may be an incomplete line — hold it for next read.
			for i, line := range lines {
				if i == len(lines)-1 {
					if len(line) > 0 {
						partial = line
					}
					break
				}
				pos += int64(len(line)) + 1
				line = bytes.TrimSpace(line)
				if len(line) == 0 {
					continue
				}
				state.lineEnd = pos
				w.emit(path, line)
			}

			state.offset += int64(n)
		}
		if err != nil {
			break
		}
	}

	// Persist any incomplete line for next call
	if len(partial) > 0 {
		state.partial = append([]byte(nil), partial...) // copy to avoid buffer reuse issues
	}
}

// emit constructs and sends an Event for a single JSONL line.
// Caller must hold w.mu.
func (w *Watcher) emit(filePath string, line []byte) {
	sessionID := sessionIDFromPath(filePath)
	projectDir := projectDirFromPath(filePath)
	label := w.labelForPath(filePath)
	id := transcript.Classify(filePath)

	ev := Event{
		SessionID:  sessionID,
		ProjectDir: projectDir,
		FilePath:   filePath,
		Line:       append([]byte(nil), line...), // copy
		Label:      label,
		Bootstrap:  w.bootstrapping,
		WorkflowID: id.WorkflowID,
		AgentID:    id.AgentID,
		AgentKind:  id.Kind,
	}

	if state, ok := w.states[filePath]; ok {
		ev.Offset = state.lineEnd
		ev.Inode = state.inode
		if !w.bootstrapping {
			state.lastEmit = time.Now()
		}
	}

	if w.bootstrapping && w.bootstrapCB != nil {
		w.bootstrapCB(ev)
		return
	}

	// Block rather than drop when the consumer falls behind: the file offset is
	// already past this line, so a dropped line would be lost until the next
	// restart. The file itself is the buffer.
	select {
	case w.events <- ev:
	case <-w.done:
		w.droppedEvents.Add(1)
	}
}

// DroppedEvents returns the number of lines discarded because the watcher was
// shutting down while the consumer was behind.
func (w *Watcher) DroppedEvents() int64 {
	return w.droppedEvents.Load()
}

// labelForPath returns the label for the base path that contains filePath.
// Caller must hold w.mu.
func (w *Watcher) labelForPath(filePath string) string {
	for base, label := range w.labels {
		expanded := expandHome(base)
		if strings.HasPrefix(filePath, expanded) {
			return label
		}
	}
	return ""
}

// sessionIDFromPath derives a session ID from the JSONL filename.
func sessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".jsonl")
}

// projectDirFromPath returns the name of the immediate parent directory.
func projectDirFromPath(path string) string {
	return filepath.Base(filepath.Dir(path))
}

// expandHome replaces a leading "~/" with the actual home directory.
// The home directory is resolved once and cached for the lifetime of the process.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	homeDirOnce.Do(func() {
		homeDirVal, homeDirErr = os.UserHomeDir()
	})
	if homeDirErr != nil {
		return path
	}
	return filepath.Join(homeDirVal, path[2:])
}
