package transcript

import "testing"

func TestClassify(t *testing.T) {
	t.Parallel()
	const root = "/home/u/.claude/projects/hash"
	tests := []struct {
		path string
		want Identity
	}{
		{root + "/abc-123.jsonl", Identity{Kind: KindTopLevel}},
		{root + "/abc-123/session.jsonl", Identity{Kind: KindTopLevel}},
		{"/tmp/test.jsonl", Identity{Kind: KindTopLevel}},
		{root + "/abc-123/subagents/agent-xyz.jsonl",
			Identity{Kind: KindSubagent, AgentID: "agent-xyz", ParentSessionID: "abc-123"}},
		{root + "/abc-123/subagents/workflows/wf_def/agent-xyz.jsonl",
			Identity{Kind: KindWorkflowAgent, AgentID: "agent-xyz", WorkflowID: "wf_def", ParentSessionID: "abc-123"}},
		{root + "/abc-123/subagents/agent-.jsonl",
			Identity{Kind: KindSubagent, AgentID: "agent-", ParentSessionID: "abc-123"}},
		{"/tmp/agent-loose.jsonl", Identity{Kind: KindSubagent, AgentID: "agent-loose"}},
		{"/tmp/wf_x/agent-loose.jsonl", Identity{Kind: KindSubagent, AgentID: "agent-loose"}},
		{root + "/abc-123/subagents/notes.jsonl", Identity{Kind: KindTopLevel, ParentSessionID: "abc-123"}},
		{root + "/gp/subagents/agent-x/subagents/agent-y.jsonl",
			Identity{Kind: KindSubagent, AgentID: "agent-y", ParentSessionID: "agent-x"}},
		// Never produced by Claude Code; locks in the filesystem-root boundary.
		{"/subagents/agent-y.jsonl", Identity{Kind: KindSubagent, AgentID: "agent-y", ParentSessionID: "/"}},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.path); got != tc.want {
				t.Errorf("Classify(%q) = %+v, want %+v", tc.path, got, tc.want)
			}
		})
	}
}
