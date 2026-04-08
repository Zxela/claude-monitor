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

const pollInterval = 5 * time.Second

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
}

// fileState tracks our read position within a single JSONL file.
type fileState struct {
	path    string
	offset  int64
	partial []byte // incomplete line carried across reads
}

// Watcher monitors JSONL files and emits Events for each new line.
type Watcher struct {
	fsw       *fsnotify.Watcher
	mu        sync.Mutex
	basePaths []string              // directories to scan
	labels    map[string]string     // basePath -> label (container name)
	states        map[string]*fileState
	events        chan Event
	droppedEvents atomic.Int64
	bootstrapping bool              // true during initial scan
	bootstrapCB   func(Event)       // called synchronously for bootstrap events
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
		_ = filepath.WalkDir(expanded, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if filepath.Base(path) == target {
				found = path
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// Start begins watching and returns a channel of Events. It runs until ctx
// is cancelled.
func (w *Watcher) Start(ctx context.Context) <-chan Event {
	go w.run(ctx)
	return w.events
}

func (w *Watcher) run(ctx context.Context) {
	defer close(w.events)

	// Initial scan — read all existing data to bootstrap session stats.
	w.mu.Lock()
	w.bootstrapping = true
	w.scanAll()
	// After tracking files, read their existing content.
	for _, state := range w.states {
		w.readNewLines(state.path)
	}
	w.bootstrapping = false
	w.mu.Unlock()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

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

		case <-ticker.C:
			// Poll for newly created JSONL files.
			w.mu.Lock()
			w.scanAll()
			w.mu.Unlock()
		}
	}
}

// scanAll walks all base paths looking for JSONL files not yet tracked.
// Caller must hold w.mu.
func (w *Watcher) scanAll() {
	for _, base := range w.basePaths {
		w.scanPath(base)
	}

	// Clean up states for files that no longer exist on disk.
	for path := range w.states {
		if _, err := os.Stat(path); os.IsNotExist(err) {
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
	_ = filepath.WalkDir(expanded, func(path string, d os.DirEntry, err error) error {
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
	})
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

	// Detect file truncation: if file is smaller than our offset, reset.
	if info, err := f.Stat(); err == nil && info.Size() < state.offset {
		log.Printf("watcher: file truncated (%s), resetting offset from %d to 0", path, state.offset)
		state.offset = 0
		state.partial = nil
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
			chunk := append(partial, buf[:n]...)
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
				line = bytes.TrimSpace(line)
				if len(line) == 0 {
					continue
				}
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

	ev := Event{
		SessionID:  sessionID,
		ProjectDir: projectDir,
		FilePath:   filePath,
		Line:       append([]byte(nil), line...), // copy
		Label:      label,
		Bootstrap:  w.bootstrapping,
	}

	if w.bootstrapping && w.bootstrapCB != nil {
		w.bootstrapCB(ev)
		return
	}

	select {
	case w.events <- ev:
	default:
		w.droppedEvents.Add(1)
		log.Printf("watcher event channel full, dropping line from %s", filePath)
	}
}

// DroppedEvents returns the total number of events dropped due to a full channel.
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
