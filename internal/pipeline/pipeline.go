// Package pipeline implements the staged event processing pipeline.
//
// Flow: Parse → Resolve Repo → Apply Session → fork(Broadcast, Buffer → Persist)
package pipeline

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
	"github.com/zxela/claude-monitor/internal/watcher"
)

const (
	flushInterval  = 2 * time.Second
	maxBatchSize   = 200
	maxMetaCache   = 500
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

	bufMu  sync.Mutex
	buffer []store.EventInsert

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
			p.sessions.Upsert(childID, func(s *session.Session) {
				if s.ParentID == "" {
					s.ParentID = parentUUID
				}
				// Back-fill repo inheritance for the child-before-parent ordering:
				// when the child arrived first, its initial event resolved its own
				// (worktree) cwd to a phantom repo because the parent was unknown.
				// Now that the parent exists, re-point the child at the parent's
				// project, mirroring applyRepoResolution's rule 1.
				if parentRepoID != "" {
					// The pin is now inherited — flag it so subsequent flushes don't
					// persist this child's worktree cwd → parent repo in cwd_repos.
					s.SetRepoInherited(true)
					if s.RepoID != parentRepoID {
						s.RepoID = parentRepoID
						s.SetRepoSourceRank(repo.SourceGitRemote)
						s.SetRepoToplevel("")
					}
				}
			})
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
		return
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
	_, exists := p.sessions.Get(ev.SessionID)
	isNew := !exists
	if isNew {
		p.loadMeta(ev)
		// Rebuild dedup state from DB if this session was previously persisted.
		// After a restart, in-memory dedup maps are empty, which would cause
		// duplicate events to double-count costs, messages, and errors.
		if ids, costs, err := p.db.LoadMessageDedup(ev.SessionID); err == nil && len(ids) > 0 {
			// Also restore session aggregates so totals aren't reset to zero.
			dbSess, _ := p.db.GetSession(ev.SessionID)
			// Recompute the cost/token aggregates from the SAME per-message map
			// just rebuilt from events, rather than copying the stored sessions
			// columns. The stored values can be stale (e.g. accumulated across
			// re-bootstraps), which would freeze a divergent total; deriving them
			// from the dedup map keeps sessions.total_cost provably equal to the
			// per-message events ground truth. See audit findings on cost drift.
			var tc float64
			var ti, to, tr, tcc int64
			for _, mc := range costs {
				tc += mc.CostUSD
				ti += mc.InputTokens
				to += mc.OutputTokens
				tr += mc.CacheReadTokens
				tcc += mc.CacheCreationTokens
			}
			// Recompute error_count deterministically from events so the badge
			// matches the feed and cannot accumulate across re-bootstraps.
			errCount, errCountErr := p.db.CountSessionErrors(ev.SessionID)
			p.sessions.Upsert(ev.SessionID, func(s *session.Session) {
				s.SetDedupState(ids, costs)
				s.TotalCost = tc
				s.InputTokens = ti
				s.OutputTokens = to
				s.CacheReadTokens = tr
				s.CacheCreationTokens = tcc
				if dbSess != nil {
					// MessageCount/EventCount are not derivable from the cost map.
					s.MessageCount = dbSess.MessageCount
					s.EventCount = dbSess.EventCount
					// Fall back to the (possibly stale) stored error_count only if
					// the deterministic recount failed.
					s.ErrorCount = dbSess.ErrorCount
				}
				if errCountErr == nil {
					s.ErrorCount = errCount
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
	})
	needFlush := len(p.buffer) >= maxBatchSize
	p.bufMu.Unlock()

	if needFlush {
		p.flush()
	}
}

// flush persists buffered events and updates session aggregates.
func (p *Pipeline) flush() {
	p.bufMu.Lock()
	if len(p.buffer) == 0 {
		p.bufMu.Unlock()
		return
	}
	batch := p.buffer
	p.buffer = nil
	p.bufMu.Unlock()

	// Persist events
	if err := p.db.PersistBatch(&store.EventBatch{Events: batch}); err != nil {
		log.Printf("batch persist error: %v", err)
	}

	// Persist affected sessions + repo mappings in a single transaction
	seen := make(map[string]bool)
	var sessionsToFlush []*session.Session
	for _, ei := range batch {
		if seen[ei.SessionID] {
			continue
		}
		seen[ei.SessionID] = true

		sess, ok := p.sessions.Get(ei.SessionID)
		if !ok {
			continue
		}
		sessionsToFlush = append(sessionsToFlush, sess)
	}
	if err := p.db.FlushSessions(sessionsToFlush); err != nil {
		log.Printf("session flush error: %v", err)
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
// metadata. Three rules, in priority order:
//
//  1. Parent inheritance: a child session (one with a known parent) inherits the
//     parent's repo_id verbatim, instead of resolving its own cwd. This stops
//     subagents running in git worktrees from minting phantom "agent-<hash>"
//     repos and fragmenting the parent run across fake projects.
//  2. Start-pin: once a (non-child) session has a repo_id, it stays pinned to
//     the START project. A later resolution may only upgrade the resolution
//     QUALITY of the SAME repository (e.g. a toplevel-basename id replaced by the
//     git-remote id for that same checkout) — never switch to a different repo,
//     even if that different repo resolved at a higher source rank.
//  3. CWD is set once (session start) and never overwritten — see below.
func (p *Pipeline) applyRepoResolution(s *session.Session, msg *parser.Event, r *repo.Repo, parentRepoID string) {
	// Rule 1: inherit the parent's project. parentRepoID is non-empty only when
	// this session has a known parent with a resolved repo_id. The child mirrors
	// the parent's CURRENT repo_id on every event — it is NOT set-once: if the
	// parent's own pin is later upgraded for the same checkout (e.g. a
	// toplevel-basename id replaced by the git-remote id), the next child event
	// re-points the child to follow it. The s.RepoID != parentRepoID guard only
	// skips a redundant write when they already agree. Start-pin (rule 2) keeps
	// the parent from flipping to a DIFFERENT repo, so this tracking can only
	// ever follow a same-repo quality upgrade, never a project switch.
	if parentRepoID != "" {
		// Mark the pin as inherited so the flush path skips recording this child's
		// (worktree) cwd → inherited repo_id in cwd_repos. That cwd belongs to the
		// child's own working tree, not the parent's project, and persisting the
		// mapping would mis-attribute future unrelated sessions that resolve the
		// same directory.
		s.SetRepoInherited(true)
		if s.RepoID != parentRepoID {
			s.RepoID = parentRepoID
			// Mark as git-remote authority: an inherited id is as trustworthy as
			// the parent's and must not be downgraded by the child's own cwd.
			s.SetRepoSourceRank(repo.SourceGitRemote)
			s.SetRepoToplevel("")
		}
	} else if r != nil {
		newRank := r.SourceRank()
		if s.RepoID == "" {
			// First resolution for this session: pin to the START project.
			s.RepoID = r.ID
			s.SetRepoSourceRank(newRank)
			s.SetRepoToplevel(r.Toplevel)
			if err := p.db.UpsertRepo(r); err != nil {
				log.Printf("upsert repo error: %v", err)
			}
		} else if r.ID != s.RepoID && newRank > s.RepoSourceRank() && sameRepo(s, r, msg.CWD) {
			// Upgrade ONLY when the new, higher-authority resolution refers to the
			// SAME repository as the pinned one (e.g. the start cwd resolved to a
			// toplevel basename because the git-remote lookup timed out, and a
			// later event for the same checkout resolves the remote origin id).
			// A higher-rank resolution for a DIFFERENT project (e.g. cd'ing into
			// another repo) is intentionally ignored so the project stays pinned
			// to where the session started. When in doubt, keep the original.
			s.RepoID = r.ID
			s.SetRepoSourceRank(newRank)
			s.SetRepoToplevel(r.Toplevel)
			if err := p.db.UpsertRepo(r); err != nil {
				log.Printf("upsert repo error: %v", err)
			}
		}
	}

	// Rule 3: CWD reflects session START, not the latest event. Set it once (the
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

// sameRepo reports whether resolution r refers to the SAME repository the
// session is already pinned to, so a higher-authority resolution may upgrade the
// id in place (rather than switch projects). newCWD is the cwd r was resolved
// from. It is deliberately conservative — it returns true only on positive
// evidence, so an ambiguous case keeps the original pinned repo.
//
// Evidence, strongest first:
//   - both sides git-backed: the incoming working-tree root is the pinned one,
//     or is NESTED WITHIN it (same-or-narrower). Two resolutions of the same
//     checkout always share a toplevel — git normalises any subdir to the repo
//     root — so the headline case is equality. The reverse direction is rejected
//     on purpose: an incoming toplevel that CONTAINS the pin is a WIDER repo
//     (e.g. the outer monorepo of a nested inner checkout), and letting an inner
//     project flip up to its enclosing repo would violate the start-pin.
//   - the session has no recorded toplevel (its pin came from a non-git fallback,
//     i.e. git was unavailable at start) but the new resolution's toplevel
//     contains the session's START cwd. That is the headline upgrade case: the
//     start cwd's git-remote lookup timed out → basename fallback, and a later
//     event for the SAME directory resolves the real git id.
//
// pathContains compares on a trailing separator so "/repo" is not treated as a
// prefix of "/repo2".
func sameRepo(s *session.Session, r *repo.Repo, newCWD string) bool {
	if r.Toplevel == "" {
		// No git root on the incoming resolution — no positive same-repo evidence.
		return false
	}
	if top := s.RepoToplevel(); top != "" {
		// Both sides are git-backed. Same repo only when the incoming root is the
		// pinned one or nested inside it. A wider incoming root that CONTAINS the
		// pin (an outer monorepo) is a different, enclosing project — reject it so
		// the inner pin cannot flip outward.
		return pathContains(top, r.Toplevel)
	}
	// The pin is a non-git fallback (no toplevel). Use the session's start cwd:
	// if it lives inside the incoming resolution's working tree, this is a
	// higher-confidence resolution of the SAME directory the session started in.
	if s.CWD != "" {
		return pathContains(r.Toplevel, s.CWD)
	}
	// Last resort: the cwd this resolution came from is the start cwd (first
	// event), so a containment check against it is still same-directory evidence.
	return newCWD != "" && pathContains(r.Toplevel, newCWD)
}

// pathContains reports whether parent is an ancestor of (or equal to) child,
// comparing on a trailing separator so "/repo" is not a prefix of "/repo2".
func pathContains(parent, child string) bool {
	if parent == child {
		return true
	}
	return strings.HasPrefix(child+string(filepath.Separator), parent+string(filepath.Separator))
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

	// Task description from first user message
	if s.TaskDescription == "" && msg.Role == "user" && msg.ContentText != "" {
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
		if sid := parentSessionIDFromPath(ev.FilePath); sid != "" && sid != ev.SessionID {
			return sid
		}
		// Neither source found a parent — record a deferred link.
		if msg.ParentUUID != "" && msg.ParentUUID != ev.SessionID {
			p.linkMu.Lock()
			p.pendingParentLinks[ev.SessionID] = msg.ParentUUID
			p.linkMu.Unlock()
		}
		return ""
	}

	// Team agent detection via parent path.
	if msg.TeamName != "" {
		return parentSessionIDFromPath(ev.FilePath)
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
func (p *Pipeline) applyCostDedup(s *session.Session, msg *parser.Event) {
	// Error tracking. Dedup on the stable event identity (message_id, else the
	// per-line uuid) so a re-emitted error line cannot be counted twice and an
	// empty-message_id error is still deduped on restart/replay.
	if msg.IsError {
		errIdentity := msg.MessageID
		if errIdentity == "" {
			errIdentity = msg.UUID
		}
		if errIdentity != "" {
			errKey := "err:" + errIdentity
			if !s.HasSeenMessageID(errKey) {
				s.MarkMessageIDSeen(errKey)
				s.ErrorCount++
			}
		} else {
			s.ErrorCount++
		}
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

	// Message count (conversation turns only)
	if msg.IsConversationTurn() {
		if msg.MessageID != "" {
			if !s.HasSeenMessageID(msg.MessageID) {
				s.MarkMessageIDSeen(msg.MessageID)
				s.MessageCount++
			}
		} else {
			s.MessageCount++
		}
	}

	// Event count (all JSONL lines)
	s.EventCount++
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

	// On compact/summary, reset message-ID dedup (IDs change after compact)
	// but preserve cost tracking to maintain running totals.
	if msg.Type == "summary" {
		s.ResetMessageIDs()
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

// parentSessionIDFromPath extracts the parent session ID from a subagent JSONL path.
// It walks UP the directory tree looking for a "subagents" segment and returns the
// directory immediately above it. This handles both:
//
//	shape 2: .../<parent-session-id>/subagents/agent-<id>.jsonl
//	shape 3: .../<parent-session-id>/subagents/workflows/wf_<id>/agent-<id>.jsonl
func parentSessionIDFromPath(filePath string) string {
	dir := filepath.Dir(filePath)
	for dir != "." && dir != string(filepath.Separator) {
		parent := filepath.Dir(dir)
		if filepath.Base(dir) == "subagents" {
			return filepath.Base(parent)
		}
		if parent == dir { // reached filesystem root; stop
			break
		}
		dir = parent
	}
	return ""
}
