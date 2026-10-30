// Package pipeline implements the staged event processing pipeline.
//
// Flow: Parse → Resolve Repo → Apply Session → fork(Broadcast, Buffer → Persist)
package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
	"github.com/zxela/claude-monitor/internal/transcript"
	"github.com/zxela/claude-monitor/internal/watcher"
)

const (
	flushInterval = 2 * time.Second
	maxBatchSize  = 200
	maxMetaCache  = 500
	// Events held for retry after PersistBatch fails.
	maxRetainedEvents = 10 * maxBatchSize
)

// BroadcastFunc is called for each processed event. It receives the event,
// session, whether this is a new session, and whether the full event detail
// should be sent (vs session-update-only).
type BroadcastFunc func(event *parser.Event, sess *session.Session, isNew bool, sendDetail bool)

// agentMeta caches subagent metadata read from .meta.json files.
type agentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	Name        string `json:"name"`
}

// Pipeline orchestrates the event processing stages.
type Pipeline struct {
	sessions  *session.Store
	db        *store.DB
	resolver  *repo.Resolver
	broadcast BroadcastFunc

	metaMu    sync.Mutex
	metaCache map[string]*agentMeta
	metaOrder []string

	linkMu             sync.Mutex
	pendingParentLinks map[string]string // childSessionID → parentUUID (not yet in session store)

	// flushMu serializes whole flushes so a retried batch can't commit after a
	// newer one for the same message (upserts are last-write-wins).
	flushMu  sync.Mutex
	bufMu    sync.Mutex
	buffer   []store.EventInsert
	retained int                         // leading buffer events kept from a failed persist
	offsets  map[string]store.FileOffset // path → read position covered by buffer
	// Files whose offsets are no longer saved this run because a batch of
	// theirs was dropped; the next start re-reads them from the last saved
	// offset.
	offsetHold map[string]bool
	dirty      map[string]bool // sessions changed without an event in buffer: metadata lines, late parent links

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New creates a Pipeline.
func New(sessions *session.Store, db *store.DB, resolver *repo.Resolver, broadcast BroadcastFunc) *Pipeline {
	p := &Pipeline{
		sessions:           sessions,
		db:                 db,
		resolver:           resolver,
		broadcast:          broadcast,
		metaCache:          make(map[string]*agentMeta),
		pendingParentLinks: make(map[string]string),
		offsets:            make(map[string]store.FileOffset),
		offsetHold:         make(map[string]bool),
		dirty:              make(map[string]bool),
		stopCh:             make(chan struct{}),
	}
	p.wg.Add(1)
	go p.flushLoop()
	return p
}

// Stop flushes remaining events and stops the background flush goroutine.
// Safe to call multiple times.
func (p *Pipeline) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	p.wg.Wait()
	p.flush() // final flush
}

// Restore loads sessions from the database into the live store, as they were
// when last persisted, so their files can be resumed from saved offsets
// instead of replayed. It returns the IDs it restored. Dedup state is loaded
// lazily by Process when a restored session receives its next line.
func (p *Pipeline) Restore(ids []string) map[string]bool {
	rows, err := p.db.RestoreSessions(ids)
	if err != nil {
		log.Printf("restore sessions: %v (re-reading their files instead)", err)
		return nil
	}
	restored := make(map[string]bool, len(rows))
	for _, r := range rows {
		p.sessions.Upsert(r.ID, func(s *session.Session) { *s = *r })
		restored[r.ID] = true
	}
	for _, r := range rows {
		if r.ParentID == "" {
			continue
		}
		if restored[r.ParentID] {
			p.sessions.LinkChild(r.ParentID, r.ID)
			continue
		}
		// The parent may still arrive from a file read in full.
		p.linkMu.Lock()
		p.pendingParentLinks[r.ID] = r.ParentID
		p.linkMu.Unlock()
	}
	return restored
}

// resolvePendingLinks checks all pending child→parent links and wires up any
// whose parent session has now been created in the session store.
func (p *Pipeline) resolvePendingLinks() {
	p.linkMu.Lock()
	defer p.linkMu.Unlock()
	for childID, parentUUID := range p.pendingParentLinks {
		// Get returns a snapshot copy, so reading parent.RepoID here is safe and
		// avoids re-entering the store lock from inside the Upsert closure below.
		if parent, ok := p.sessions.Get(parentUUID); ok {
			parentRepoID := parent.RepoID
			changed := false
			p.sessions.Upsert(childID, func(s *session.Session) {
				if s.ParentID == "" {
					s.ParentID = parentUUID
					changed = true
				}
				// The child arrived before its parent, so its first event pinned
				// its own (worktree) cwd. Re-point it at the parent's project.
				if parentRepoID != "" {
					before := s.RepoPin()
					after := before.Inherit(parentRepoID)
					changed = changed || after != before
					s.SetRepoPin(after)
				}
			})
			// The child may have no further lines, so nothing else would persist
			// this, and a restart that restores it from the DB would undo it.
			if changed {
				p.bufMu.Lock()
				p.dirty[childID] = true
				p.bufMu.Unlock()
			}
			p.sessions.LinkChild(parentUUID, childID)
			delete(p.pendingParentLinks, childID)
		}
	}
}

// Process handles a single watcher event through the full pipeline.
func (p *Pipeline) Process(ev watcher.Event) {
	// Stage 1: Parse
	event, err := parser.ParseLine(ev.Line)
	if err != nil {
		if !ev.Bootstrap {
			log.Printf("parse error (%s): %v (line prefix: %.120s)", ev.FilePath, err, ev.Line)
		}
		p.markRead(ev)
		return
	}
	if event.IsInternal() {
		p.markRead(ev)
		return
	}
	// Lines with neither uuid nor message.id (queue-operation, custom-title,
	// agent-name, ...) would all share the empty dedup key and overwrite one
	// row per session. Key them by content so a re-read upserts the same row.
	if event.UUID == "" && event.MessageID == "" {
		sum := sha256.Sum256(ev.Line)
		event.UUID = "line-" + hex.EncodeToString(sum[:16])
	}

	// Stage 2: Resolve Repo
	var resolvedRepo *repo.Repo
	if event.CWD != "" {
		var resolveErr error
		resolvedRepo, resolveErr = p.resolver.Resolve(event.CWD, ev.Label)
		if resolveErr != nil {
			log.Printf("debug: repo resolve (%s): %v", event.CWD, resolveErr)
		}
	}

	// Pre-read subagent meta.json (only for new sessions).
	existing, exists := p.sessions.Get(ev.SessionID)
	isNew := !exists
	if isNew || existing.DedupPending() {
		p.loadMeta(ev)
		// Rebuild dedup state from DB if this session was previously persisted.
		// After a restart, in-memory dedup maps are empty, which would cause
		// duplicate events to double-count costs, messages, and errors.
		// Gate on the session existing in the DB (not on len(costs) > 0): a
		// session whose only persisted events are errors has no costed
		// rows, but its err: dedup keys and error_count still need rebuilding
		// or replay re-inflates the count on every restart.
		costs, costsErr := p.db.LoadMessageCosts(ev.SessionID)
		totals, totalsErr := p.db.SessionTotals(ev.SessionID)
		dedupErr := errors.Join(costsErr, totalsErr)
		dbSess, getErr := p.db.GetSession(ev.SessionID)
		if !isNew && (dedupErr != nil || getErr != nil) {
			// Applying this line to the restored totals would be undone when a
			// later line seeds from committed rows that don't include it. Skip it
			// and stop saving the file's offset so the next start re-reads it.
			log.Printf("load dedup state for restored session %s: %v (skipping line; file re-read on next start)", ev.SessionID, errors.Join(dedupErr, getErr))
			p.bufMu.Lock()
			if ev.FilePath != "" {
				p.offsetHold[ev.FilePath] = true
				delete(p.offsets, ev.FilePath)
			}
			p.bufMu.Unlock()
			return
		}
		if dedupErr == nil && (len(costs) > 0 || dbSess != nil) {
			// Seed the "err:" dedup keys from persisted error events. Without
			// them, bootstrap replay of the JSONL re-fires ErrorCount++ for every
			// historical error line on top of the recounted baseline below —
			// doubling sessions.error_count on every restart.
			errIDs, errIDsErr := p.db.LoadErrorDedup(ev.SessionID)
			// Recompute error_count deterministically from events so the badge
			// matches the feed and cannot accumulate across re-bootstraps.
			errCount, errCountErr := p.db.CountSessionErrors(ev.SessionID)
			// Same for event/message counts: seed the row keys so replayed lines
			// are not counted again on top of the restored totals.
			rowKeys, evCount, turnCount, rowErr := p.db.LoadRowKeys(ev.SessionID)
			if rowErr == nil && errIDsErr == nil {
				for _, id := range errIDs {
					rowKeys["err:"+id] = struct{}{}
				}
			}
			p.sessions.Upsert(ev.SessionID, func(s *session.Session) {
				s.SetDedupPending(false)
				s.RestoreMessageCosts(costs)
				// The stored sessions columns can be stale; the events are the ground truth.
				s.TotalCost = totals.CostUSD
				s.InputTokens = totals.InputTokens
				s.OutputTokens = totals.OutputTokens
				s.CacheReadTokens = totals.CacheReadTokens
				s.CacheCreationTokens = totals.CacheCreationTokens
				if dbSess != nil {
					// MessageCount/EventCount are not derivable from the cost map.
					s.MessageCount = dbSess.MessageCount
					s.EventCount = dbSess.EventCount
					// Fall back to the (possibly stale) stored error_count only if
					// the deterministic recount failed.
					s.ErrorCount = dbSess.ErrorCount
					// Restore the identity columns too. The main Upsert below re-stamps
					// these from the watcher event (set-once), so they are usually
					// re-derived on this same event — but a first post-restart line that
					// carries no parent signal (e.g. a lone summary line) would otherwise
					// broadcast the subagent with an empty ParentID until a later
					// sidechain line arrives. Copying from the DB row keeps the
					// in-memory session consistent with persisted state immediately.
					s.ParentID = dbSess.ParentID
					s.WorkflowID = dbSess.WorkflowID
					s.AgentID = dbSess.AgentID
					s.AgentKind = dbSess.AgentKind
					s.AITitle = dbSess.AITitle
					s.PRLinks = dbSess.PRLinks
					// A restored session already holds the full row, with exact times.
					if isNew && event.IsSessionMetadata() {
						// A metadata line sets nothing else, and Docker sources start at
						// EOF, so without these the flush writes blanks over the saved row.
						saved := dbSess.ToSession()
						s.RepoID, s.SessionName, s.CWD = saved.RepoID, saved.SessionName, saved.CWD
						s.GitBranch, s.Model, s.TaskDescription = saved.GitBranch, saved.Model, saved.TaskDescription
						s.Version, s.Entrypoint = saved.Version, saved.Entrypoint
						s.StartedAt, s.LastActive = saved.StartedAt, saved.LastActive
					}
				}
				if errCountErr == nil {
					s.ErrorCount = errCount
				}
				if rowErr == nil {
					s.SetSeenRows(rowKeys)
					s.EventCount = evCount
					s.MessageCount = turnCount
				}
			})
		}
	}

	// Pre-resolve parent UUID outside of Upsert to avoid re-entrant lock.
	// If msg.ParentUUID names a session already in the store, use it directly;
	// otherwise fall back to path inference. A "" result means defer to later.
	resolvedParentID := p.resolveParentID(event, ev)

	// Pre-resolve the parent session's repo_id (also outside the Upsert lock to
	// avoid re-entering the session-store lock). Child sessions inherit their
	// parent's project so subagents running in git worktrees don't mint phantom
	// "agent-<hash>" repos from their own worktree cwd. Empty means "unknown
	// parent" — fall back to normal cwd resolution.
	parentRepoID := p.lookupParentRepoID(resolvedParentID)

	// Stage 3: Apply Session
	sess := p.sessions.Upsert(ev.SessionID, func(s *session.Session) {
		p.applyEvent(s, event, ev, resolvedRepo, resolvedParentID, parentRepoID)
	})

	// Link child to parent
	if sess.ParentID != "" {
		p.sessions.LinkChild(sess.ParentID, ev.SessionID)
	}

	// Resolve any deferred child→parent links now that this session exists in the store.
	// Must run AFTER the Upsert above so the current session is visible to lookups.
	p.resolvePendingLinks()

	if event.IsSessionMetadata() {
		if p.broadcast != nil && !ev.Bootstrap {
			p.broadcast(event, sess, isNew, false)
		}
		p.bufMu.Lock()
		p.dirty[ev.SessionID] = true
		p.noteOffsetLocked(ev)
		p.bufMu.Unlock()
		return
	}

	// Stage 4a: Broadcast (immediate)
	if p.broadcast != nil && !ev.Bootstrap {
		sendDetail := !skipDetail(event)
		p.broadcast(event, sess, isNew, sendDetail)
	}

	// Stage 4b: Buffer for batch persist
	fullContent := event.FullContent
	if fullContent == "" && len(event.ContentText) > 200 {
		fullContent = event.ContentText
	}

	p.bufMu.Lock()
	p.buffer = append(p.buffer, store.EventInsert{
		SessionID:   ev.SessionID,
		Event:       event,
		FullContent: fullContent,
		Thinking:    event.ThinkingContent,
	})
	p.noteOffsetLocked(ev)
	needFlush := len(p.buffer)-p.retained >= maxBatchSize
	p.bufMu.Unlock()

	if needFlush {
		p.flush()
	}
}

// markRead records that ev's line needs nothing persisted, so its file offset
// can be saved with the next batch.
func (p *Pipeline) markRead(ev watcher.Event) {
	p.bufMu.Lock()
	p.noteOffsetLocked(ev)
	p.bufMu.Unlock()
}

// noteOffsetLocked records ev's end offset. Caller must hold bufMu and must
// already have buffered ev's event, if it has one: a flush saves this offset
// together with the buffer, so it never covers a line that isn't persisted.
func (p *Pipeline) noteOffsetLocked(ev watcher.Event) {
	if ev.FilePath == "" || ev.Offset <= 0 || p.offsetHold[ev.FilePath] {
		return
	}
	p.offsets[ev.FilePath] = store.FileOffset{
		Path:      ev.FilePath,
		SessionID: ev.SessionID,
		Offset:    ev.Offset,
		Inode:     ev.Inode,
	}
}

// flush persists buffered events, the sessions they touched and the file
// offsets they cover in one transaction.
func (p *Pipeline) flush() {
	p.flushMu.Lock()
	defer p.flushMu.Unlock()

	p.bufMu.Lock()
	if len(p.buffer) == 0 && len(p.offsets) == 0 && len(p.dirty) == 0 {
		p.bufMu.Unlock()
		return
	}
	batch := p.buffer
	p.buffer = nil
	p.retained = 0
	offsets := make([]store.FileOffset, 0, len(p.offsets))
	for _, o := range p.offsets {
		offsets = append(offsets, o)
	}
	clear(p.offsets)
	ids := make([]string, 0, len(batch)+len(p.dirty))
	for id := range p.dirty {
		ids = append(ids, id)
	}
	clear(p.dirty)
	p.bufMu.Unlock()

	for _, ei := range batch {
		ids = append(ids, ei.SessionID)
	}
	seen := make(map[string]bool)
	var sessionsToFlush []*session.Session
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true

		sess, ok := p.sessions.Get(id)
		if !ok {
			continue
		}
		sessionsToFlush = append(sessionsToFlush, sess)
	}

	// On failure put the batch back for the next flush; past maxRetainedEvents
	// give up so a dead database can't grow memory forever.
	err := p.db.PersistBatch(&store.EventBatch{Events: batch, Sessions: sessionsToFlush, Offsets: offsets})
	if err == nil {
		return
	}
	p.bufMu.Lock()
	defer p.bufMu.Unlock()
	for _, id := range ids {
		p.dirty[id] = true
	}
	if len(batch)+len(p.buffer) <= maxRetainedEvents {
		p.buffer = append(batch, p.buffer...)
		p.retained = len(batch)
		for _, o := range offsets {
			if _, newer := p.offsets[o.Path]; !newer {
				p.offsets[o.Path] = o
			}
		}
		log.Printf("batch persist error, will retry %d events: %v", len(batch), err)
		return
	}
	log.Printf("batch persist error, dropping %d events (retry buffer full): %v", len(batch), err)
	// A later batch's offset for these files would skip the lines lost here.
	for _, o := range offsets {
		p.offsetHold[o.Path] = true
		delete(p.offsets, o.Path)
	}
}

func (p *Pipeline) flushLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.flush()
		}
	}
}

// applyEvent updates session state from a parsed event.
// It delegates to focused helpers for each concern.
// resolvedParentID is the pre-computed parent session ID (may be empty).
// parentRepoID is the pre-resolved repo_id of that parent (may be empty); when
// set, a child session inherits it instead of resolving its own (worktree) cwd.
func (p *Pipeline) applyEvent(s *session.Session, msg *parser.Event, ev watcher.Event, r *repo.Repo, resolvedParentID, parentRepoID string) {
	if msg.IsSessionMetadata() {
		applySessionMetadata(s, msg)
		return
	}
	p.applyRepoResolution(s, msg, r, parentRepoID)
	p.applySessionMeta(s, msg, ev)
	p.applyParentDetection(s, msg, ev, resolvedParentID)
	p.applyWorkflowIdentity(s, ev)
	p.applyCostDedup(s, msg)
	p.applyStatusUpdate(s, msg, ev)
}

// applyWorkflowIdentity records the workflow/agent identity derived by the
// watcher from the file path. AgentKind/AgentID/WorkflowID are set-once (never
// overwritten with empty) so a later non-identity line cannot clear them.
func (p *Pipeline) applyWorkflowIdentity(s *session.Session, ev watcher.Event) {
	if ev.AgentKind != "" && s.AgentKind == "" {
		s.AgentKind = ev.AgentKind
	}
	if ev.AgentID != "" && s.AgentID == "" {
		s.AgentID = ev.AgentID
	}
	if ev.WorkflowID != "" && s.WorkflowID == "" {
		s.WorkflowID = ev.WorkflowID
	}
}

// applyRepoResolution attributes the session's repo_id and copies branch/model
// metadata. A Child session with a known parent inherits the parent's repo_id
// instead of resolving its own (worktree) cwd, so subagents don't mint phantom
// "agent-<hash>" repos. Otherwise repo.Pin.Resolve start-pins the session.
func (p *Pipeline) applyRepoResolution(s *session.Session, msg *parser.Event, r *repo.Repo, parentRepoID string) {
	if parentRepoID != "" {
		s.SetRepoPin(s.RepoPin().Inherit(parentRepoID))
	} else if r != nil {
		// s.CWD is still the start cwd: it is set below, once.
		if pin, adopted := s.RepoPin().Resolve(r, s.CWD, msg.CWD); adopted {
			s.SetRepoPin(pin)
			if err := p.db.UpsertRepo(r); err != nil {
				log.Printf("upsert repo error: %v", err)
			}
		}
	}

	// CWD reflects session START, not the latest event. Set it once (the
	// first event that carries a cwd) and never overwrite it, so a card's shown
	// directory cannot drift away from the project it's attributed to (e.g. after
	// a `cd`). Repo resolution above still uses the per-event msg.CWD, so this
	// does not affect attribution.
	if msg.CWD != "" && s.CWD == "" {
		s.CWD = msg.CWD
	}
	if msg.GitBranch != "" {
		s.GitBranch = msg.GitBranch
	}
	// Ignore the '<synthetic>' placeholder (a 0-token internal Claude Code model
	// string) so the session keeps its dominant real model. Otherwise real
	// (e.g. opus) spend gets mislabeled under a fake model in the Model Mix
	// chart and trends API.
	if msg.Model != "" && msg.Model != "<synthetic>" {
		s.Model = msg.Model
	}
	if msg.Version != "" && s.Version == "" {
		s.Version = msg.Version
	}
	if msg.Entrypoint != "" && s.Entrypoint == "" {
		s.Entrypoint = msg.Entrypoint
	}
}

// applySessionMeta updates session naming, source file tracking, and task description.
func (p *Pipeline) applySessionMeta(s *session.Session, msg *parser.Event, ev watcher.Event) {
	// Track which file is providing events for this session.
	if s.SourceFile == "" {
		s.SourceFile = ev.FilePath
	} else if s.SourceFile != ev.FilePath {
		log.Printf("session %s: source file changed from %s to %s", ev.SessionID, s.SourceFile, ev.FilePath)
		s.SourceFile = ev.FilePath
	}

	// Session naming
	if msg.Type == "custom-title" && msg.ContentText != "" {
		s.SessionName = msg.ContentText
	} else if msg.Type == "agent-name" && msg.ContentText != "" && s.SessionName == "" {
		s.SessionName = msg.ContentText
	}

	// Task description from the first user message that is actual user prose.
	// Local-command echoes and harness-injected blocks (<local-command-caveat>,
	// <command-name>, <system-reminder>, …) arrive as user-role lines before the
	// real task; taking one of those produced garbage Task lines in History and
	// the autopsy. Skip them and wait for the next user message.
	if s.TaskDescription == "" && msg.Role == "user" && msg.ContentText != "" &&
		!isHarnessInjectedContent(msg.ContentText) {
		desc := msg.ContentText
		if len([]rune(desc)) > 200 {
			desc = string([]rune(desc)[:200])
		}
		s.TaskDescription = desc

		// Fallback name for subagents without meta.json
		if s.ParentID != "" && s.SessionName == "" {
			aid := s.ID
			aid = strings.TrimPrefix(aid, "agent-")
			if len(aid) > 8 {
				aid = aid[:8]
			}
			s.SessionName = "agent " + aid
		}
	}
}

// applySessionMetadata records ai-title and pr-link lines. It deliberately
// skips cost, counts, status and timestamps so they never show as activity.
func applySessionMetadata(s *session.Session, msg *parser.Event) {
	switch msg.Type {
	case "ai-title":
		if msg.AITitle != "" {
			s.AITitle = msg.AITitle
		}
	case "pr-link":
		if msg.PRNumber > 0 && msg.PRURL != "" {
			s.SetPRLink(session.PRLink{Number: msg.PRNumber, URL: msg.PRURL, Repository: msg.PRRepository})
		}
	}
}

// resolveParentID determines the parent session ID for a subagent event.
// It is called OUTSIDE of any session store lock to avoid re-entrancy deadlocks.
// When msg.ParentUUID names a known session, that UUID is returned directly.
// Otherwise path-based inference is used as a fallback.
// If neither source yields a result but msg.ParentUUID is set, a deferred link
// is stored so it can be resolved once the parent session arrives.
// Returns the resolved parent session ID, or "" if the link must be deferred.
func (p *Pipeline) resolveParentID(msg *parser.Event, ev watcher.Event) string {
	if !msg.IsSidechain && msg.ParentUUID == "" && msg.TeamName == "" {
		return ""
	}

	// For subagent events: prefer the in-content sessionId, then direct UUID lookup.
	if msg.IsSidechain || msg.ParentUUID != "" {
		// Preferred: the in-content sessionId of a sidechain line is the TRUE
		// parent session UUID for both task subagents (shape 2) and workflow
		// agents (shape 3). The watcher keyed this row on the filename stem
		// (ev.SessionID = agent-<id>), so when they differ, msg.SessionID names
		// the parent. Guard against self-parenting. This is authoritative and
		// does not depend on the parent already existing in the store, so a
		// child arriving before its parent is still linked immediately.
		if msg.IsSidechain && msg.SessionID != "" && msg.SessionID != ev.SessionID {
			// Also record a deferred parent→child backlink. The child's own
			// ParentID is set immediately, but parent.Children is only ever
			// populated by LinkChild, which fires here (line 166) when the
			// parent already exists, or via resolvePendingLinks once the parent
			// arrives. During bootstrap/replay a finished subagent's lines may
			// all be read BEFORE the parent's top-level line, so without this
			// entry the parent's Children array would never be backfilled.
			// resolvePendingLinks' `if s.ParentID == ""` guard keeps this
			// idempotent with the ParentID already set on the child, and it
			// deletes the entry once the parent→child edge is wired.
			p.linkMu.Lock()
			if _, ok := p.pendingParentLinks[ev.SessionID]; !ok {
				p.pendingParentLinks[ev.SessionID] = msg.SessionID
			}
			p.linkMu.Unlock()
			return msg.SessionID
		}
		if msg.ParentUUID != "" {
			if _, ok := p.sessions.Get(msg.ParentUUID); ok {
				return msg.ParentUUID
			}
		}
		// Fall back to path-based inference.
		if sid := transcript.Classify(ev.FilePath).ParentSessionID; sid != "" && sid != ev.SessionID {
			return sid
		}
		// Neither source found a parent — record a deferred link. Guard on !ok so
		// this matches the in-content sibling's set-once policy above and never
		// overwrites an already-recorded pending link for this child.
		if msg.ParentUUID != "" && msg.ParentUUID != ev.SessionID {
			p.linkMu.Lock()
			if _, ok := p.pendingParentLinks[ev.SessionID]; !ok {
				p.pendingParentLinks[ev.SessionID] = msg.ParentUUID
			}
			p.linkMu.Unlock()
		}
		return ""
	}

	// Team agent detection via parent path.
	if msg.TeamName != "" {
		return transcript.Classify(ev.FilePath).ParentSessionID
	}

	return ""
}

// lookupParentRepoID returns the parent session's repo_id so a child can inherit
// it, or "" if there is no (known) parent. It checks the live session store
// first (the common case: parent processed before its subagents) and falls back
// to the persisted sessions table (e.g. the parent was flushed in an earlier run
// or is being replayed out of order). MUST be called OUTSIDE the session-store
// Upsert lock — Get takes the store's RLock and would otherwise deadlock.
func (p *Pipeline) lookupParentRepoID(parentID string) string {
	if parentID == "" {
		return ""
	}
	if parent, ok := p.sessions.Get(parentID); ok && parent.RepoID != "" {
		return parent.RepoID
	}
	if row, err := p.db.GetSession(parentID); err == nil && row != nil && row.RepoID != "" {
		return row.RepoID
	}
	return ""
}

// applyParentDetection sets ParentID for subagents and team agents.
// resolvedParentID is pre-computed by resolveParentID before the Upsert lock is held.
func (p *Pipeline) applyParentDetection(s *session.Session, msg *parser.Event, ev watcher.Event, resolvedParentID string) {
	if resolvedParentID == "" || s.ParentID != "" {
		return
	}

	s.ParentID = resolvedParentID

	// Apply meta-derived name for subagents.
	if msg.IsSidechain || msg.ParentUUID != "" {
		p.metaMu.Lock()
		meta := p.metaCache[ev.SessionID]
		p.metaMu.Unlock()
		if meta != nil {
			name := meta.Name
			if name == "" {
				name = meta.AgentType
			}
			if name != "" && s.SessionName == "" {
				s.SessionName = name
			}
		}
	}

	// Apply agent name for team agents.
	if msg.TeamName != "" && msg.AgentName != "" && s.SessionName == "" {
		s.SessionName = msg.AgentName
	}
}

// applyCostDedup handles error tracking, cost/token dedup, and message counting.
// isHarnessInjectedContent reports whether a user-role message body is a
// harness artifact rather than user prose: local-command echoes, caveat
// wrappers, system reminders, and task notifications all arrive as user-role
// lines and must not become a session's TaskDescription.
func isHarnessInjectedContent(text string) bool {
	t := strings.TrimSpace(text)
	for _, tag := range []string{
		"<local-command-", "<command-", "<system-reminder>",
		"<task-notification>", "<user-prompt-submit-hook>",
	} {
		if strings.HasPrefix(t, tag) {
			return true
		}
	}
	return false
}

func (p *Pipeline) applyCostDedup(s *session.Session, msg *parser.Event) {
	key := msg.MessageID
	if key == "" {
		key = msg.UUID
	}

	// Error tracking, deduped on the stable event identity so a re-emitted or
	// replayed error line is counted once.
	if msg.IsError && (key == "" || s.MarkRowSeen("err:"+key)) {
		s.ErrorCount++
	}

	// Cost/token dedup
	if msg.MessageID != "" && (msg.CostUSD > 0 || msg.InputTokens > 0 || msg.OutputTokens > 0) {
		prev := s.GetMessageCosts(msg.MessageID)
		s.TotalCost += msg.CostUSD - prev.CostUSD
		s.InputTokens += msg.InputTokens - prev.InputTokens
		s.OutputTokens += msg.OutputTokens - prev.OutputTokens
		s.CacheReadTokens += msg.CacheReadTokens - prev.CacheReadTokens
		s.CacheCreationTokens += msg.CacheCreationTokens - prev.CacheCreationTokens
		s.SetMessageCosts(msg.MessageID, session.MessageCosts{
			CostUSD:             msg.CostUSD,
			InputTokens:         msg.InputTokens,
			OutputTokens:        msg.OutputTokens,
			CacheReadTokens:     msg.CacheReadTokens,
			CacheCreationTokens: msg.CacheCreationTokens,
		})
	}

	// One event row per dedup key: streamed chunks of a message share it, and
	// the startup replay of persisted lines finds it already seen.
	if key == "" || s.MarkRowSeen(key) {
		s.EventCount++
		if msg.IsConversationTurn() {
			s.MessageCount++
		}
	}
}

// applyStatusUpdate updates timestamps, handles summary resets, and sets session status.
func (p *Pipeline) applyStatusUpdate(s *session.Session, msg *parser.Event, ev watcher.Event) {
	// Timestamps
	if !msg.Timestamp.IsZero() {
		s.LastActive = msg.Timestamp
		if s.StartedAt.IsZero() || msg.Timestamp.Before(s.StartedAt) {
			s.StartedAt = msg.Timestamp
		}
	} else if !ev.Bootstrap {
		s.LastActive = time.Now()
	}

	// Status
	switch {
	case msg.Type == "summary":
		s.Status = "thinking" // session stays active through compact
	case msg.StopReason == "end_turn":
		s.Status = "waiting"
	case msg.StopReason == "tool_use":
		s.Status = "tool_use"
	case msg.ToolName != "":
		s.Status = "tool_use"
	case msg.Role == "assistant":
		s.Status = "thinking"
	case msg.Role == "user":
		s.Status = "thinking"
	}
}

func (p *Pipeline) loadMeta(ev watcher.Event) {
	p.metaMu.Lock()
	defer p.metaMu.Unlock()
	if _, cached := p.metaCache[ev.SessionID]; cached {
		return
	}
	metaPath := strings.TrimSuffix(ev.FilePath, ".jsonl") + ".meta.json"
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta agentMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			log.Printf("debug: unmarshal meta.json (session=%s, path=%s): %v", ev.SessionID, metaPath, err)
		} else {
			p.metaCache[ev.SessionID] = &meta
			p.metaOrder = append(p.metaOrder, ev.SessionID)
		}
	}
	if len(p.metaCache) > maxMetaCache {
		half := len(p.metaOrder) / 2
		for _, key := range p.metaOrder[:half] {
			delete(p.metaCache, key)
		}
		p.metaOrder = p.metaOrder[half:]
	}
}

// skipDetail returns true for events that should only trigger a session_update,
// not a full event broadcast.
func skipDetail(event *parser.Event) bool {
	switch event.Type {
	case "file-history-snapshot", "queue-operation":
		return true
	case "system":
		// Allow turn_duration through; skip other system subtypes.
		return event.Subtype != "turn_duration"
	}
	return false
}
