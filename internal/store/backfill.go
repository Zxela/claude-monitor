package store

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zxela/claude-monitor/internal/parser"
)

// backfillMarkerKey is the settings key that records whether the v013 identity
// backfill has completed successfully. Value "1" means done; "" or absent means
// the backfill should run on the next startup.
const backfillMarkerKey = "backfill_v013_done"

// agentFileRe matches a Claude Code agent transcript file base name:
// "agent-<id>.jsonl". The captured group is the bare agent id.
var agentFileRe = regexp.MustCompile(`^agent-(.+)\.jsonl$`)

// Agent-kind classifications. These mirror the watcher's package-private
// constants (internal/watcher/watcher.go:39-44) and pipeline path classification;
// keep them in sync.
const (
	backfillKindSubagent      = "subagent"
	backfillKindWorkflowAgent = "workflow_agent"
)

// BackfillResult summarises a v013 identity backfill run.
type BackfillResult struct {
	Scanned int // agent-*.jsonl files inspected
	Updated int // sessions whose UPDATE touched a row
	Skipped int // files with no matching session row or no parent line
	Errors  int // recoverable per-file errors (walk continues)
}

// DefaultBackfillBasePaths returns the well-known locations Claude Code writes
// session files, with ~/ expanded via os.UserHomeDir. This intentionally keeps
// its own copy of the list because the watcher's defaultBasePaths/expandHome are
// unexported; keep it in sync with internal/watcher/watcher.go:46-51.
func DefaultBackfillBasePaths() []string {
	paths := []string{
		"/home/node/.claude/projects",
		"/root/.claude/projects",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append([]string{filepath.Join(home, ".claude", "projects")}, paths...)
	}
	return paths
}

// classifyAgentPath derives (kind, agentID, workflowID) from a transcript file
// path. It mirrors the watcher's classifyPath / pipeline.parentSessionIDFromPath
// rules so backfill labels rows identically to live ingestion:
//
//	shape 1 normal:        <uuid>.jsonl                                           -> ("",                 "",           "")
//	shape 2 task subagent: <parent>/subagents/agent-<id>.jsonl                    -> ("subagent",         "agent-<id>", "")
//	shape 3 workflow:      <parent>/subagents/workflows/wf_<id>/agent-<id>.jsonl  -> ("workflow_agent",   "agent-<id>", "wf_<id>")
//
// A non-agent file (no "agent-" prefix) returns kind == "" so callers skip it.
func classifyAgentPath(filePath string) (kind, agentID, workflowID string) {
	base := filepath.Base(filePath)
	m := agentFileRe.FindStringSubmatch(base)
	if m == nil {
		return "", "", ""
	}
	// agentID is the full "agent-<id>" stem (== ev.SessionID and the session
	// row's id for shapes 2/3), matching the watcher's classifyPath so backfilled
	// and live-ingested rows store an identical agent_id value. m[1] is the bare
	// captured id, used only to confirm the match.
	_ = m[1]
	agentID = strings.TrimSuffix(base, ".jsonl")

	// Walk UP looking for a wf_<id> segment and/or a subagents segment.
	hasSubagents := false
	wf := ""
	dir := filepath.Dir(filePath)
	for dir != "." && dir != string(filepath.Separator) {
		seg := filepath.Base(dir)
		if seg == "subagents" {
			hasSubagents = true
		}
		if wf == "" && strings.HasPrefix(seg, "wf_") {
			wf = seg
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached filesystem root; stop
			break
		}
		dir = parent
	}
	if hasSubagents && wf != "" {
		return backfillKindWorkflowAgent, agentID, wf
	}
	// shape 2, or defensive fallback (agent-* file without a subagents/ ancestor):
	// classify as a subagent so it is never mistaken for a top-level session.
	return backfillKindSubagent, agentID, ""
}

// firstValidEventSessionID reads the first non-empty, parseable JSONL line from
// the file and returns its in-content sessionId (the parent UUID for shapes 2/3).
// Blank and unparseable lines are skipped, matching the watcher's trim/skip
// behaviour. Returns "" if no valid line yields a session id.
func firstValidEventSessionID(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Allow long JSONL lines (default 64K is too small for transcript content).
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ev, err := parser.ParseLine([]byte(line))
		if err != nil || ev == nil {
			continue
		}
		if ev.SessionID != "" {
			return ev.SessionID, nil
		}
	}
	return "", sc.Err()
}

// RunBackfillV013 walks the given base paths for agent-*.jsonl transcripts and
// backfills the parent_id / workflow_id / agent_id / agent_kind columns
// (added by migration 013) for rows that predate Workstream 2 identity stamping
// or were ingested while those columns were empty.
//
// It is idempotent and marker-guarded:
//   - if !force and the backfill_v013_done marker == "1", it returns early with a
//     zero-walk result (no reads, no writes).
//   - the UPDATE only fills empty/blank columns (CASE guards), so re-runs never
//     clobber values set by live ingestion or a prior run.
//   - the marker is set ONLY after a fully successful walk, so a mid-walk failure
//     leaves the marker unset and the next startup retries.
//
// It never deletes or truncates rows; it only writes the four identity columns.
func (d *DB) RunBackfillV013(basePaths []string, force bool) (BackfillResult, error) {
	var res BackfillResult

	if !force {
		if v, err := d.GetSetting(backfillMarkerKey); err == nil && v == "1" {
			// Marker already set: short-circuit with no walk and no writes.
			return res, nil
		}
	}

	// Guarded, additive UPDATE: only fills columns that are currently empty so a
	// second run (or live ingestion having already populated them) changes nothing.
	// Depends on migration 013 having added workflow_id/agent_id/agent_kind.
	const updateSQL = `UPDATE sessions SET
		parent_id  = CASE WHEN COALESCE(parent_id,'')  = '' THEN ? ELSE parent_id  END,
		workflow_id = CASE WHEN COALESCE(workflow_id,'') = '' THEN ? ELSE workflow_id END,
		agent_id   = CASE WHEN COALESCE(agent_id,'')   = '' THEN ? ELSE agent_id   END,
		agent_kind = CASE WHEN COALESCE(agent_kind,'') = '' THEN ? ELSE agent_kind END
		WHERE id = ?`

	for _, base := range basePaths {
		if base == "" {
			continue
		}
		// Silently skip non-existent base paths, mirroring watcher.scanPath.
		if _, err := os.Stat(base); err != nil {
			continue
		}

		walkErr := filepath.WalkDir(base, func(path string, dirent os.DirEntry, err error) error {
			if err != nil {
				// Unreadable dir entry: count as a recoverable error and continue.
				res.Errors++
				return nil
			}
			if dirent.IsDir() {
				return nil
			}
			if !agentFileRe.MatchString(filepath.Base(path)) {
				return nil
			}

			res.Scanned++

			kind, agentID, workflowID := classifyAgentPath(path)
			if kind == "" {
				// Not an agent transcript shape we backfill.
				return nil
			}
			// The session row id is the "agent-<id>" file stem (ev.SessionID key).
			id := strings.TrimSuffix(filepath.Base(path), ".jsonl")

			parentID, ferr := firstValidEventSessionID(path)
			if ferr != nil {
				res.Errors++
				return nil
			}
			// Guard against self-parenting: the in-content sessionId must differ
			// from this row's own id to be a real parent link. A missing parent
			// line is fine — we still write the identity columns (agent_id/kind/
			// workflow_id) so the row is correctly classified.
			if parentID == id {
				parentID = ""
			}

			result, uerr := d.db.Exec(updateSQL, parentID, workflowID, agentID, kind, id)
			if uerr != nil {
				res.Errors++
				return nil
			}
			affected, _ := result.RowsAffected()
			if affected > 0 {
				res.Updated++
				log.Printf("backfill: linked %s parent=%s workflow=%s kind=%s", id, parentID, workflowID, kind)
			} else {
				// No row with this id (session not ingested yet) — count Skipped.
				res.Skipped++
			}
			return nil
		})
		if walkErr != nil {
			// A fatal walk error: return WITHOUT setting the marker so we retry.
			return res, walkErr
		}
	}

	// Only set the marker after a fully successful walk (force runs also re-set it
	// so a subsequent non-forced start short-circuits again).
	if err := d.SetSetting(backfillMarkerKey, "1"); err != nil {
		return res, err
	}

	log.Printf("backfill v013: scanned=%d updated=%d skipped=%d errors=%d",
		res.Scanned, res.Updated, res.Skipped, res.Errors)
	return res, nil
}
