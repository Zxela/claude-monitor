// Package transcript derives a Session's identity from its transcript file path.
package transcript

import (
	"path/filepath"
	"strings"
)

// Session kinds, stored as sessions.agent_kind.
const (
	KindTopLevel      = "session"
	KindSubagent      = "subagent"
	KindWorkflowAgent = "workflow_agent"
)

// Identity is what a transcript path says about its Session.
type Identity struct {
	Kind string
	// AgentID is the "agent-<id>" file stem; empty for a Top-level session.
	AgentID string
	// WorkflowID is the "wf_<id>" directory of a Workflow agent; empty otherwise.
	WorkflowID string
	// ParentSessionID is the directory above the nearest "subagents" segment.
	// It is set for any file under subagents/, including a non-agent file that
	// Kind reports as top-level.
	ParentSessionID string
}

// Classify maps the on-disk shapes:
//
//	<uuid>.jsonl                                          -> top-level
//	<parent>/subagents/agent-<id>.jsonl                   -> subagent, parent
//	<parent>/subagents/workflows/wf_<id>/agent-<id>.jsonl -> workflow agent, parent, wf_<id>
//
// An agent-* file with no subagents/ ancestor is a Subagent with no parent, so
// it is never mistaken for a Top-level session.
func Classify(path string) Identity {
	var id Identity
	dir := filepath.Dir(path)
	for dir != "." && dir != string(filepath.Separator) {
		parent := filepath.Dir(dir)
		base := filepath.Base(dir)
		if base == "subagents" && id.ParentSessionID == "" {
			id.ParentSessionID = filepath.Base(parent)
		}
		if id.WorkflowID == "" && strings.HasPrefix(base, "wf_") {
			id.WorkflowID = base
		}
		if parent == dir {
			break
		}
		dir = parent
	}

	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if !strings.HasPrefix(stem, "agent-") {
		return Identity{Kind: KindTopLevel, ParentSessionID: id.ParentSessionID}
	}
	id.AgentID = stem
	if id.ParentSessionID != "" && id.WorkflowID != "" {
		id.Kind = KindWorkflowAgent
		return id
	}
	id.Kind = KindSubagent
	id.WorkflowID = ""
	return id
}
