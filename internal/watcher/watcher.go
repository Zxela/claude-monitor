// Package watcher watches Claude Code JSONL session files for new content.
package watcher

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
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
}

// fileState tracks our read position within a single JSONL file.
type fileState struct {
	path   string
	offset int64
}

// Watcher monitors JSONL files and emits Events for each new line.
type Watcher struct {
	fsw       *fsnotify.Watcher
	basePaths []string         // directories to scan
	states    map[string]*fileState
	events    chan Event
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
		states:    make(map[string]*fileState),
		events:    make(chan Event, 512),
	}, nil
}

// Start begins watching and returns a channel of Events. It runs until ctx
// is cancelled.
func (w *Watcher) Start(ctx context.Context) <-chan Event {
	go w.run(ctx)
	return w.events
}

func (w *Watcher) run(ctx context.Context) {
	defer close(w.events)

	// Initial scan.
	w.scanAll()

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
				w.readNewLines(ev.Name)
			}

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watcher fsnotify error: %v", err)

		case <-ticker.C:
			// Poll for newly created JSONL files.
			w.scanAll()
		}
	}
}

// scanAll walks all base paths looking for JSONL files not yet tracked.
func (w *Watcher) scanAll() {
	for _, base := range w.basePaths {
		expanded := expandHome(base)
		if _, err := os.Stat(expanded); os.IsNotExist(err) {
			continue
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
}

// ensureTracked registers a JSONL file for watching if not already tracked.
// For new files we seek to the end so we only emit lines written after startup.
func (w *Watcher) ensureTracked(path string) {
	if _, ok := w.states[path]; ok {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}

	// Start reading from the current end of file so we don't replay history.
	w.states[path] = &fileState{path: path, offset: info.Size()}

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

	if _, err := f.Seek(state.offset, 0); err != nil {
		log.Printf("watcher seek error (%s): %v", path, err)
		return
	}

	buf := make([]byte, 64*1024)
	var partial []byte

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
}

// emit constructs and sends an Event for a single JSONL line.
func (w *Watcher) emit(filePath string, line []byte) {
	sessionID := sessionIDFromPath(filePath)
	projectDir := projectDirFromPath(filePath)

	select {
	case w.events <- Event{
		SessionID:  sessionID,
		ProjectDir: projectDir,
		FilePath:   filePath,
		Line:       append([]byte(nil), line...), // copy
	}:
	default:
		log.Printf("watcher event channel full, dropping line from %s", filePath)
	}
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
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
